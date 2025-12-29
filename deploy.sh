
#!/bin/bash
set -e

# --- CONFIGURATION ---
DOMAIN="quic.myip.gr"
EMAIL="admin@$DOMAIN"
MASTER_CONF_URL="https://raw.githubusercontent.com/chrismfz/mynginx/refs/heads/main/nginx.conf.master"

# Paths
SRC_DIR="/usr/local/src"
NGINX_PATH="/opt/nginx"
AWS_LC_PATH="/opt/aws-lc"
HTML_PATH="/opt/nginx/html"

echo "--- 1. Installing Build Dependencies & Tools ---"
apt update
apt install -y build-essential cmake git curl wget perl golang-go gpg lsb-release ca-certificates apt-transport-https \
               zlib1g-dev libpcre2-dev libssl-dev certbot net-tools sudo libmcrypt-dev mcrypt acl libreadline-dev libunwind-dev 

echo "--- 2. Fetching Source Code & Master Config ---"
mkdir -p $SRC_DIR
mkdir -p $NGINX_PATH/conf
mkdir -p $NGINX_PATH/conf/sites
mkdir -p $NGINX_PATH/logs
mkdir -p $NGINX_PATH/cache
mkdir -p $NGINX_PATH/cache/proxy_micro
mkdir -p $NGINX_PATH/cache/proxy_static
mkdir -p $NGINX_PATH/cache/fastcgi

mkdir -p $HTML_PATH

chown -R www-data:www-data /opt/nginx/logs
chown -R www-data:www-data /opt/nginx/cache


# Clone or Update Repos
declare -A REPOS=(
    ["aws-lc"]="https://github.com/aws/aws-lc.git"
    ["ngx_brotli"]="https://github.com/google/ngx_brotli.git"
    ["nginx"]="https://github.com/nginx/nginx.git"

    # Lua support
    ["ngx_devel_kit"]="https://github.com/vision5/ngx_devel_kit.git"
    ["lua-nginx-module"]="https://github.com/openresty/lua-nginx-module.git"
    ["LuaJIT"]="https://github.com/LuaJIT/LuaJIT.git"

)

for repo in "${!REPOS[@]}"; do
    cd $SRC_DIR
    if [ -d "$repo" ]; then
        echo "Updating $repo..."
        cd "$repo" && git pull && git submodule update --init && cd ..
    else
        echo "Cloning $repo..."
        git clone --recursive "${REPOS[$repo]}"
    fi
done

# Download your Master Config from GitHub
echo "Downloading master config template..."

curl -sL $MASTER_CONF_URL -o $NGINX_PATH/conf/nginx.conf


echo "--- 3. Compiling AWS-LC (The Crypto Engine) ---"
cd $SRC_DIR/aws-lc
rm -rf build && mkdir build && cd build
cmake .. -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX=$AWS_LC_PATH -DBUILD_SHARED_LIBS=OFF
cmake --build . --target install

# --- 3.5 Prepare Brotli ---
echo "--- Preparing Brotli Dependencies ---"
cd $SRC_DIR/ngx_brotli/deps/brotli
rm -rf out && mkdir out && cd out
cmake .. -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF
cmake --build . --config Release --target brotlienc brotlicommon brotlidec

# --- 3.6 Prepare LUA ---
echo "--- Building LuaJIT ---"
cd $SRC_DIR/LuaJIT
make -j"$(nproc)"
make install PREFIX=/opt/luajit

# Make sure the runtime linker can find it (for dynamic libluajit)
echo "/opt/luajit/lib" > /etc/ld.so.conf.d/luajit.conf
ldconfig

export LUAJIT_LIB=/opt/luajit/lib
export LUAJIT_INC="$(ls -d /opt/luajit/include/luajit-* | head -n1)"

# Optional safety check (fail fast)
if [ -z "$LUAJIT_INC" ] || [ ! -d "$LUAJIT_INC" ]; then
  echo "ERROR: Could not find LuaJIT include dir under /opt/luajit/include/"
  ls -la /opt/luajit/include/ || true
  exit 1
fi



# --- 4. Compiling NGINX ---
echo "--- Compiling NGINX ---"
cd $SRC_DIR/nginx

# We need to tell NGINX exactly where those new brotli and awc-lc libraries are
./auto/configure \
    --prefix=$NGINX_PATH \
    --with-debug \
    --with-http_ssl_module \
    --with-http_v2_module \
    --with-http_v3_module \
    --with-cc-opt="-I$AWS_LC_PATH/include -I$SRC_DIR/ngx_brotli/deps/brotli/c/include -I$LUAJIT_INC" \
    --with-ld-opt="-L$AWS_LC_PATH/lib -L$SRC_DIR/ngx_brotli/deps/brotli/out -L$LUAJIT_LIB -Wl,-rpath,/opt/luajit/lib -lluajit-5.1 -lm -ldl -lssl -lcrypto -lstdc++" \
    --with-pcre-jit \
    --with-threads \
    --with-file-aio \
    --with-http_gzip_static_module \
    --add-module=$SRC_DIR/ngx_brotli \
    --add-module=$SRC_DIR/ngx_devel_kit \
    --add-module=$SRC_DIR/lua-nginx-module

make -j$(nproc)
make install

echo "--- 5. System Integration (Systemd Unit) ---"
cat <<EOF > /etc/systemd/system/nginx.service
[Unit]
Description=The NGINX HTTP and reverse proxy server (Custom Build)
After=network-online.target
Wants=network-online.target

[Service]
Type=forking
PIDFile=$NGINX_PATH/logs/nginx.pid
ExecStartPre=/opt/nginx/sbin/nginx -t -c /opt/nginx/conf/nginx.conf
ExecStart=/opt/nginx/sbin/nginx -c /opt/nginx/conf/nginx.conf
ExecReload=/opt/nginx/sbin/nginx -s reload -c /opt/nginx/conf/nginx.conf
ExecStop=/bin/kill -s QUIT \$MAINPID
PrivateTmp=true
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF



echo "--- 6. Install PHP 8.3  ---"
# Add the packages.sury.org/php repository.
sudo apt-get update
sudo apt-get install -y lsb-release ca-certificates apt-transport-https curl
sudo curl -sSLo /tmp/debsuryorg-archive-keyring.deb https://packages.sury.org/debsuryorg-archive-keyring.deb
sudo dpkg -i /tmp/debsuryorg-archive-keyring.deb
sudo sh -c 'echo "deb [signed-by=/usr/share/keyrings/debsuryorg-archive-keyring.gpg] https://packages.sury.org/php/ $(lsb_release -sc) main" > /etc/apt/sources.list.d/php.list'
sudo apt-get update

# Install PHP.
apt install --no-install-recommends  -y php8.3-bcmath php8.3-bz2 php8.3-cli php8.3-common php8.3-curl php8.3-decimal php8.3-enchant php8.3-fpm php8.3-gd php8.3-grpc php8.3-igbinary php8.3-imagick php8.3-imap php8.3-inotify php8.3-lz4 php8.3-mailparse php8.3-maxminddb php8.3-mbstring php8.3-mcrypt php8.3-memcache php8.3-memcached php8.3-mysql php8.3-opcache php8.3-protobuf php8.3-redis php8.3-rrd php8.3-soap php8.3-sqlite3  php8.3-tidy php8.3-uploadprogress php8.3-uuid php8.3-xml php8.3-xmlrpc  php8.3-yaml php8.3-zip php8.3-zstd


systemctl daemon-reload
systemctl enable --now nginx


echo "--- Setting Up Logrotate ---"
cat <<EOF > /etc/logrotate.d/nginx-custom
$NGINX_PATH/logs/*.log {
    daily
    rotate 14
    compress
    notifempty
    create 0640 www-data www-data
    sharedscripts
    postrotate
        [ -f $NGINX_PATH/logs/nginx.pid ] && kill -USR1 \$(cat $NGINX_PATH/logs/nginx.pid)
    endscript
}
EOF


cd /opt/
git clone https://github.com/chrismfz/mynginx.git


echo "--- DEPLOYMENT COMPLETE ---"

/opt/nginx/sbin/nginx -V 2>&1


