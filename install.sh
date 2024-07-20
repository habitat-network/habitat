#!/usr/bin/env bash

# Note, this script is intended to be executed on user machines via curl | bash
# This is a very simple way to install the script, but it is also very bad security
# practice to execute scripts from the internet without verifying them first.
# We are doing this for convenience, but don't take this as a good example of how
# to do things. You should always verify the contents of a script before executing it.

# Usage:
#
# Use latest version:
#
#   curl -sL https://github.com/eagraf/habitat-new/releases/latest/download/install.sh 2>&1 | bash
#
# Use specific version:
#
#   curl -sL https://github.com/eagraf/habitat-new/releases/download/v0.0.2/install.sh 2>&1 | bash

set -euxo pipefail

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
if [ "$ARCH" == "x86_64" ]; then
    ARCH="amd64"
fi

VERSION="latest"
if [ -n "$1" ]; then
    VERSION=$1
fi

if [[ "$VERSION" == "latest" ]]; then
    ARCHIVE_URL="https://github.com/eagraf/habitat-new/releases/latest/download/habitat-${ARCH}-${OS}.tar.gz"
else
    ARCHIVE_URL="https://github.com/eagraf/habitat-new/releases/download/${VERSION}/habitat-${ARCH}-${OS}.tar.gz"
fi

TMP_DIR=$(mktemp -d)
echo "Downloading to $TMP_DIR"
curl -L $ARCHIVE_URL -o $TMP_DIR/habitat-${ARCH}-${OS}.tar.gz
mkdir -p $TMP_DIR/habitat-${ARCH}-${OS}
tar -xzf $TMP_DIR/habitat-${ARCH}-${OS}.tar.gz -C $TMP_DIR/habitat-${ARCH}-${OS}


BIN_PATH="$HOME/.habitat/bin"
mkdir -p $BIN_PATH

cp $TMP_DIR/habitat-${ARCH}-${OS}/habitat $BIN_PATH/habitat

CERT_DIR="$HOME/.habitat/certificates"
mkdir -p "$CERT_DIR"

touch $HOME/.habitat/habitat.yml

read -p "Would you like to generate a new user identity key? [y/n]" -n 1 -r < /dev/tty
echo
if [[ $REPLY =~ ^[Yy]$ ]] ; then
    openssl req -newkey rsa:2048 \
        -new -nodes -x509 \
        -out "$CERT_DIR/dev_node_cert.pem" \
        -keyout "$CERT_DIR/dev_node_key.pem" \
        -subj "/C=US/ST=California/L=Mountain View/O=Habitat/CN=dev_node"
fi

read -p "Would you like to generate a new user identity key? [y/n]" -n 1 -r < /dev/tty
echo
if [[ $REPLY =~ ^[Yy]$ ]] ; then
    openssl req -newkey rsa:2048 \
        -new -nodes -x509 \
        -out "$CERT_DIR/dev_root_user_cert.pem" \
        -keyout "$CERT_DIR/dev_root_user_key.pem" \
        -subj "/C=US/ST=California/L=Mountain View/O=Habitat/CN=dev_node"
fi
