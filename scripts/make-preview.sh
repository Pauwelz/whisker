#!/bin/sh
# Make preview.png (README and marketplace card) from two demo screenshots.
# Usage: scripts/make-preview.sh <chat-list.png> <chat.png>
# Needs rsvg-convert and the JetBrainsMono Nerd Font.
set -eu
[ $# -eq 2 ] || { echo "usage: $0 <chat-list.png> <chat.png>" >&2; exit 2; }
root=$(cd "$(dirname "$0")/.." && pwd)
list=$(base64 -w0 "$1")
chat=$(base64 -w0 "$2")
logo=$(base64 -w0 "$root/assets/whisker.svg")
font="JetBrainsMono Nerd Font"

cat <<EOF | rsvg-convert -w 1600 -h 900 -o "$root/preview.png"
<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="900" viewBox="0 0 1600 900">
  <defs>
    <radialGradient id="glow" cx="0.78" cy="0.45" r="0.7">
      <stop offset="0" stop-color="#3a2f1a"/>
      <stop offset="0.55" stop-color="#151a20"/>
      <stop offset="1" stop-color="#0c0f13"/>
    </radialGradient>
    <pattern id="dots" width="28" height="28" patternUnits="userSpaceOnUse">
      <circle cx="2" cy="2" r="1.2" fill="#ffffff" fill-opacity="0.05"/>
    </pattern>
    <filter id="shadow" x="-20%" y="-20%" width="140%" height="140%">
      <feDropShadow dx="0" dy="24" stdDeviation="28" flood-color="#000" flood-opacity="0.6"/>
    </filter>
    <clipPath id="round"><rect width="486" height="600" rx="12"/></clipPath>
  </defs>

  <rect width="1600" height="900" fill="url(#glow)"/>
  <rect width="1600" height="900" fill="url(#dots)"/>

  <image href="data:image/svg+xml;base64,$logo" x="100" y="250" width="104" height="104"/>
  <text x="100" y="450" font-family="$font" font-weight="700" font-size="84" fill="#f2f2f2">whisker</text>
  <text x="100" y="515" font-family="$font" font-size="30" fill="#c9ced6">WhatsApp in your</text>
  <text x="100" y="557" font-family="$font" font-size="30" fill="#F5B83D">Omarchy bar.</text>
  <g font-family="$font" font-size="20" fill="#8b93a0">
    <text x="100" y="625">› read and reply from the pop-out</text>
    <text x="100" y="657">› media, quotes and reactions</text>
    <text x="100" y="689">› encrypted on disk</text>
  </g>
  <text x="100" y="810" font-family="$font" font-size="15" fill="#5d6570">Unofficial · built on whatsmeow · not affiliated with WhatsApp or Meta</text>

  <!-- The chat overlaps only the empty lower part of the chat list, so every Chat line stays visible. -->
  <g transform="translate(700 40)" filter="url(#shadow)">
    <image href="data:image/png;base64,$list" width="486" height="600" clip-path="url(#round)" preserveAspectRatio="none"/>
  </g>
  <g transform="translate(1040 270)" filter="url(#shadow)">
    <image href="data:image/png;base64,$chat" width="486" height="600" clip-path="url(#round)" preserveAspectRatio="none"/>
  </g>
</svg>
EOF
