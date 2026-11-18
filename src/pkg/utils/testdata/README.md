# Audio fixture

`no-id3.mp3` is a 1,344-byte MP3 containing a synthetic 440 Hz sine wave.
It has no ID3 metadata. FFprobe identifies MP3 audio at 48,000 Hz with one
channel, and FFmpeg decodes the complete file without errors.

Generated with FFmpeg 9.0.2 and libmp3lame:

```sh
ffmpeg -hide_banner -loglevel error -f lavfi -i sine=frequency=440:duration=0.10 -ar 48000 -ac 1 -c:a libmp3lame -id3v2_version 0 -write_id3v1 0 -y no-id3.mp3
```

SHA-256: `0eb6d9ecaf9e33019bbe2f6a8194eb5642ee98b9af0d90fda5016f70a09978eb`.

The fixture is committed so the MIME regression runs without FFmpeg installed.
