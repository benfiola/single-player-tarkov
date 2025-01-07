#!/bin/bash
set -e
cwd="$(pwd)"
spt_server_patch="$(readlink -f "$(dirname "${BASH_SOURCE[0]}")")/server-spt.patch"

echo "build spt server"
rm -rf /tmp/source
git clone https://github.com/sp-tarkov/server.git /tmp/source 
cd /tmp/source 
git checkout "${SPT_SERVER_VERSION}" 
git apply "${spt_server_patch}"
cd /tmp/source/project 
npm install 
npm run build:release 
mv build /server 
cd "${cwd}"
rm -rf /tmp/source 

echo "install fika server"
curl -o /tmp/archive.zip -fsSL "https://github.com/project-fika/Fika-Server/releases/download/v${FIKA_SERVER_VERSION}/fika-server.zip" 
unzip /tmp/archive.zip -d /server 
rm -rf /tmp/archive.zip 
