#!/usr/bin/env python3
"""Build an unsigned local Jellytool.app with offline plugin resources."""
import argparse
import os
from pathlib import Path
import plistlib
import re
import shutil
import subprocess
import sys
from urllib.parse import urlsplit

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--bun', type=Path, required=True, help='local Bun 1.4.2 executable')
parser.add_argument('--output', type=Path, required=True, help='new Jellytool.app path')
parser.add_argument('--version', default='dev', help='Jellytool release version, e.g. 0.1.0 (default: dev)')
parser.add_argument('--build', default='', help='CFBundleVersion build marker, e.g. 20261115.1')
parser.add_argument('--update-feed', default='', help='HTTPS URL of the Jellytool update JSON')
args = parser.parse_args()
if args.version != 'dev' and not re.fullmatch(r'(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', args.version):
    parser.error('version must be dev or a release version such as 0.1.0')
if args.build and not re.fullmatch(r'[0-9]+(?:\.[0-9]+)*', args.build):
    parser.error('build must contain only numeric dot-separated components')
try:
    feed = urlsplit(args.update_feed)
    valid_feed = feed.scheme == 'https' and bool(feed.hostname) and feed.username is None and not feed.fragment
except ValueError:
    valid_feed = False
if args.update_feed and (not valid_feed or re.search(r'''[\s'"\\]''', args.update_feed)):
    parser.error('update-feed must be an HTTPS URL without credentials, fragments or whitespace')
if args.version != 'dev' and not args.update_feed and not args.build:
    parser.error('a release version requires --update-feed, unless --build is set for a local production artifact')
if args.version == 'dev' and args.build:
    parser.error('a build marker requires a release version')
if sys.platform != 'darwin':
    parser.error('this packager currently builds macOS apps only')
root = Path(__file__).resolve().parents[2]
output = args.output.resolve()
bun = args.bun.resolve(strict=True)
if output.exists() or output.suffix != '.app':
    parser.error('output must be a new .app path')
macos = output / 'Contents/MacOS'
resources = output / 'Contents/Resources'
macos.mkdir(parents=True)
resources.mkdir()
ldflags = f'-X main.version={args.version}'
if args.update_feed:
    ldflags += f' -X github.com/yetone/magpie/internal/update.JellytoolFeed={args.update_feed}'
# Bun 1.4.2 needs macOS 13, so the cgo objects and LSMinimumSystemVersion
# target it too (package-multi.py's MACOS_MIN).
macos_min = '13.0'
go_env = dict(os.environ, MACOSX_DEPLOYMENT_TARGET=macos_min)
for variable in ('CGO_CFLAGS', 'CGO_LDFLAGS'):
    go_env[variable] = (os.environ.get(variable, '-O2 -g') + f' -mmacosx-version-min={macos_min}').strip()
subprocess.run(['go', 'build', '-tags', 'jellytool,production', '-ldflags', ldflags, '-o', str(macos / 'jellytool'), '.'], cwd=root, env=go_env, check=True)
subprocess.run(['go', 'run', './cmd/jellytool-pack', str(bun), str(resources / 'jellytool')], cwd=root, check=True)
# Use the same A2 app artwork as the native window; keep the supplier logo separate.
iconset = output.parent / 'jellytool.iconset'
iconset.mkdir()
try:
    for size in (16, 32, 128, 256, 512):
        for scale in (1, 2):
            name = f'icon_{size}x{size}' + ('@2x' if scale == 2 else '') + '.png'
            subprocess.run(['sips', '-z', str(size * scale), str(size * scale),
                            str(root / 'internal/gui/assets/icons/jellytool-app.png'),
                            '--out', str(iconset / name)], check=True, stdout=subprocess.DEVNULL)
    subprocess.run(['iconutil', '-c', 'icns', str(iconset), '-o', str(resources / 'jellytool.icns')], check=True)
finally:
    shutil.rmtree(iconset)
shutil.copyfile(root / 'internal/gui/assets/jellytool-guide.md', resources / '使用说明.md')
shutil.copyfile(root / 'internal/gui/assets/jellytool-guide.md', output.parent / '使用说明.md')
shutil.copyfile(root / 'internal/gui/assets/jellytool-models.json', output.parent / 'model-recommendations.example.json')
shutil.copyfile(root / 'docs/plans/jellytool-model-recommendations.md', output.parent / '模型推荐配置说明.md')
shutil.copyfile(root / 'LICENSE', resources / 'LICENSE')
intro, separator, notices = (root / 'THIRD_PARTY_NOTICES').read_text().partition('\n----------------')
(resources / 'THIRD_PARTY_NOTICES').write_text(intro.replace('magpie', 'Jellytool') + separator + notices)
# Finder shows the bundle's file name unless the bundle names itself per
# language. macOS uses the localized name only while the file name matches
# the plain CFBundleDisplayName, so both stay Jellytool (the path updates use).
for lang in ["Base", "en", "zh-Hans", "zh_CN", "zh-Hant", "zh_TW", "ja", "de"]:
    (resources / f'{lang}.lproj').mkdir()
    (resources / f'{lang}.lproj/InfoPlist.strings').write_text('CFBundleName = "Jellytoken助手";\nCFBundleDisplayName = "Jellytoken助手";\n', encoding='utf-8')
with (output / 'Contents/Info.plist').open('wb') as f:
    plistlib.dump({'CFBundleName': 'Jellytool', 'CFBundleDisplayName': 'Jellytool',
                  'CFBundleIdentifier': 'com.jellytoken.jellytool.dev', 'CFBundleExecutable': 'jellytool',
                  'CFBundleIconFile': 'jellytool.icns', 'CFBundlePackageType': 'APPL',
                  'CFBundleShortVersionString': args.version if args.version != 'dev' else '0.0.0',
                  'CFBundleVersion': args.build or (args.version if args.version != 'dev' else '0.0.0'),
                  'LSMinimumSystemVersion': macos_min, 'NSHighResolutionCapable': True, 'LSHasLocalizedDisplayName': True}, f)
print(output)
