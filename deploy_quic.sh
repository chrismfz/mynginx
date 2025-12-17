
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
apt install -y build-essential cmake git curl wget perl golang-go \
               zlib1g-dev libpcre2-dev libssl-dev certbot net-tools

echo "--- 2. Fetching Source Code & Master Config ---"
mkdir -p $SRC_DIR
mkdir -p $NGINX_PATH/conf
mkdir -p $HTML_PATH

# Clone or Update Repos
declare -A REPOS=(
    ["aws-lc"]="https://github.com/aws/aws-lc.git"
    ["ngx_brotli"]="https://github.com/google/ngx_brotli.git"
    ["nginx"]="https://github.com/nginx/nginx.git"
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
curl -sL $MASTER_CONF_URL -o $NGINX_PATH/conf/nginx.conf.master

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
    --with-cc-opt="-I$AWS_LC_PATH/include -I$SRC_DIR/ngx_brotli/deps/brotli/c/include" \
    --with-ld-opt="-L$AWS_LC_PATH/lib -L$SRC_DIR/ngx_brotli/deps/brotli/out -lssl -lcrypto -lstdc++" \
    --with-pcre-jit \
    --with-threads \
    --with-file-aio \
    --with-http_gzip_static_module \
    --add-module=$SRC_DIR/ngx_brotli

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
ExecStartPre=$NGINX_PATH/sbin/nginx -t
ExecStart=$NGINX_PATH/sbin/nginx
ExecReload=$NGINX_PATH/sbin/nginx -s reload
ExecStop=/bin/kill -s QUIT \$MAINPID
PrivateTmp=true
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload


echo "--- 5.5 Creating Temporary Self-Signed Certificate ---"
# Create the directory for the certificates
mkdir -p /etc/letsencrypt/live/$DOMAIN

# Generate a fast self-signed cert so NGINX can pass its syntax check
openssl req -x509 -nodes -days 1 -newkey rsa:2048 \
    -keyout /etc/letsencrypt/live/$DOMAIN/privkey.pem \
    -out /etc/letsencrypt/live/$DOMAIN/fullchain.pem \
    -subj "/CN=$DOMAIN"

# Ensure NGINX can read them
chmod 600 /etc/letsencrypt/live/$DOMAIN/privkey.pem



echo "--- 6. Initial Config Deployment (Pre-SSL) ---"
# Generate the live config from the master template
cp $NGINX_PATH/conf/nginx.conf.master $NGINX_PATH/conf/nginx.conf
sed -i "s/{{DOMAIN}}/$DOMAIN/g" $NGINX_PATH/conf/nginx.conf

# Start NGINX so Certbot can perform the webroot challenge
systemctl enable --now nginx

echo "--- 7. SSL Setup (Webroot Mode - Zero Downtime) ---"
# Request ECDSA cert using the existing HTML directory for the ACME challenge
certbot certonly --webroot \
    -w $HTML_PATH \
    --non-interactive --agree-tos --email "$EMAIL" \
    --key-type ecdsa \
    -d "$DOMAIN" \
    --force-renewal \
    --deploy-hook "$NGINX_PATH/sbin/nginx -s reload"

echo "--- 8. Security: Firewall (UFW) ---"
#TODO#

echo "--- 9. Setting Up Logrotate ---"
cat <<EOF > /etc/logrotate.d/nginx-custom
$NGINX_PATH/logs/*.log {
    daily
    rotate 14
    compress
    notifempty
    create 0640 root root
    sharedscripts
    postrotate
        [ -f $NGINX_PATH/logs/nginx.pid ] && kill -USR1 \$(cat $NGINX_PATH/logs/nginx.pid)
    endscript
}
EOF

# Final reload to ensure SSL is picked up
$NGINX_PATH/sbin/nginx -s reload

echo "--- DEPLOYMENT COMPLETE ---"
echo "Test your HTTP/3 here: https://http3check.net/?host=$DOMAIN"
