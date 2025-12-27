package util

import (
        "fmt"
        "os"
        "path/filepath"
        "bytes"
        "context"
        "os/exec"
	"time"
        "crypto/sha256"
        "encoding/hex"


)

// MkdirAll is a small wrapper so callers can use util.MkdirAll consistently.
func MkdirAll(dir string, perm os.FileMode) error {
        if dir == "" || dir == "." {
                return nil
        }
        if err := os.MkdirAll(dir, perm); err != nil {
                return fmt.Errorf("mkdirall %s: %w", dir, err)
        }
        return nil
}


func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
        dir := filepath.Dir(path)
        if err := MkdirAll(dir, 0755); err != nil {
                return err
        }
        tmp, err := os.CreateTemp(dir, ".tmp-*")
        if err != nil {
                return fmt.Errorf("create temp in %s: %w", dir, err)
        }
        tmpName := tmp.Name()

        ok := false
        defer func() {
                tmp.Close()
                if !ok {
                        _ = os.Remove(tmpName)
                }
        }()

        if err := tmp.Chmod(perm); err != nil {
                return fmt.Errorf("chmod temp: %w", err)
        }
        if _, err := tmp.Write(data); err != nil {
                return fmt.Errorf("write temp: %w", err)
        }
        if err := tmp.Sync(); err != nil {
                return fmt.Errorf("sync temp: %w", err)
        }
        if err := tmp.Close(); err != nil {
                return fmt.Errorf("close temp: %w", err)
        }

        if err := os.Rename(tmpName, path); err != nil {
                return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
        }

        ok = true
        return nil
}


// exec //
type Result struct {
        Stdout   string
        Stderr   string
        ExitCode int
}

func Run(timeout time.Duration, name string, args ...string) (Result, error) {
        ctx, cancel := context.WithTimeout(context.Background(), timeout)
        defer cancel()

        cmd := exec.CommandContext(ctx, name, args...)

        var outb, errb bytes.Buffer
        cmd.Stdout = &outb
        cmd.Stderr = &errb

        err := cmd.Run()

        res := Result{
                Stdout: outb.String(),
                Stderr: errb.String(),
        }

        // Timeout?
        if ctx.Err() == context.DeadlineExceeded {
                res.ExitCode = -1
                return res, fmt.Errorf("command timeout after %s: %s %v", timeout, name, args)
        }

        if err == nil {
                res.ExitCode = 0
                return res, nil
        }

        // Non-zero exit
        if ee, ok := err.(*exec.ExitError); ok {
                res.ExitCode = ee.ExitCode()
                return res, fmt.Errorf("command failed (exit %d): %s %v", res.ExitCode, name, args)
        }

        return res, fmt.Errorf("command error: %s %v: %w", name, args, err)
}



//hashx//
func Sha256Hex(b []byte) string {
        h := sha256.Sum256(b)
        return hex.EncodeToString(h[:])
}

