#!/bin/bash
# KZ 2025.12.03

set -e

echo "[+] Building barix_discovery Go binaries..."

# Linux (x86_64)
GOOS=linux GOARCH=amd64 go build -o barix_discovery_linux barix_discovery.go

# Windows (x86_64)
GOOS=windows GOARCH=amd64 go build -o barix_discovery.exe barix_discovery.go

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o barix_discovery_mac_amd64 barix_discovery.go

# macOS (ARM)
GOOS=darwin GOARCH=arm64 go build -o barix_discovery_mac_arm64 barix_discovery.go

# Android (ARM64)
GOOS=android GOARCH=arm64 go build -o barix_discovery_android_arm64 barix_discovery.go

echo "[+] Creating ZIP bundle of all binaries..."

timestamp=$(date +"%Y_%m_%d_%H_%M_%S")
bundle_name="barix_discovery_bundle__${timestamp}.zip"

zip -q "${bundle_name}" \
    barix_discovery_linux \
    barix_discovery.exe \
    barix_discovery_mac_amd64 \
    barix_discovery_mac_arm64 \
    barix_discovery_android_arm64

echo "[+] Done: ${bundle_name}"
