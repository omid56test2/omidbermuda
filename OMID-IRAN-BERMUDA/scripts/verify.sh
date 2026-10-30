#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo '[1/7] Go race tests'
go test -race ./...
echo '[2/7] go vet'
go vet ./...
echo '[3/7] Linux AMD64 build'
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/omid-iran-bermuda-linux-amd64 ./cmd/server
echo '[4/7] Windows AMD64 cross-build'
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o dist/omid-iran-bermuda-windows-amd64.exe ./cmd/server
echo '[5/7] QR JavaScript syntax'
if command -v node >/dev/null 2>&1; then
  node --check web/bermuda-qr.js
  echo '[6/7] Inline admin-page JavaScript syntax'
  python3 - <<'PY'
from html.parser import HTMLParser
from pathlib import Path
import subprocess, tempfile
class Scripts(HTMLParser):
    def __init__(self):
        super().__init__(); self.active=False; self.external=False; self.buf=[]; self.items=[]
    def handle_starttag(self, tag, attrs):
        if tag.lower() == 'script':
            self.active=True; self.external=any(k.lower()=='src' for k,_ in attrs); self.buf=[]
    def handle_data(self, data):
        if self.active and not self.external: self.buf.append(data)
    def handle_endtag(self, tag):
        if tag.lower() == 'script' and self.active:
            if not self.external: self.items.append(''.join(self.buf))
            self.active=False; self.external=False; self.buf=[]
p=Scripts(); p.feed(Path('web/index.html').read_text(encoding='utf-8'))
with tempfile.TemporaryDirectory() as tmp:
    for i, source in enumerate(p.items):
        f=Path(tmp)/f'inline-{i}.js'; f.write_text(source, encoding='utf-8')
        subprocess.run(['node','--check',str(f)],check=True)
print(f'checked {len(p.items)} inline script block(s)')
PY
else
  echo 'Node.js missing; skipped JavaScript syntax checks.'
fi
echo '[7/7] JSON fixtures and release metadata'
python3 - <<'PY'
import json
from pathlib import Path
cfg=json.loads(Path('configs/xray.example.json').read_text())
assert cfg['inbounds']==[]
assert Path('VERSION').read_text().strip()=='1.0.1'
print('JSON fixture and version metadata are valid')
PY
echo 'Verification completed.'
