"""Refresh the locally hosted Google Fonts. Run manually, not during builds."""
from pathlib import Path
from urllib.request import Request, urlopen
import re

assets = Path(__file__).resolve().parents[1] / 'assets'
font_dir = assets / 'fonts'
font_dir.mkdir(exist_ok=True)
url = 'https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;450;500;550;600;650;700;750;800&family=IBM+Plex+Mono:wght@400;450;500;600&display=swap'
request = Request(url, headers={'User-Agent': 'Mozilla/5.0'})
css = urlopen(request).read().decode()
urls = list(dict.fromkeys(re.findall(r'url\((https://[^)]+)\)', css)))
for i, font_url in enumerate(urls):
    extension = font_url.split('?')[0].rsplit('.', 1)[-1]
    name = f'correlux-font-{i}.{extension}'
    (font_dir / name).write_bytes(urlopen(font_url).read())
    css = css.replace(font_url, f'fonts/{name}')
(assets / 'fonts.css').write_text(css)
for name, license_url in {
    'DM-Sans-OFL.txt':'https://raw.githubusercontent.com/google/fonts/main/ofl/dmsans/OFL.txt',
    'IBM-Plex-Mono-OFL.txt':'https://raw.githubusercontent.com/google/fonts/main/ofl/ibmplexmono/OFL.txt'
}.items():
    (font_dir / name).write_bytes(urlopen(license_url).read())
print(f'Downloaded {len(urls)} font files and both OFL licences.')
