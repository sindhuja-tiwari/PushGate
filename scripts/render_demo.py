#!/usr/bin/env python3
"""Render state/demo/record.jsonl (from scripts/demo.py) into a 90-second
video: the live fleet on the left, the commands and their output on the
right, a caption on top.

Nothing in the video is drawn from made-up data. Every tile is an instance
from a registry snapshot taken during the run, and every terminal line is
output the run printed. Time runs at 1x, except that the video pauses on each
caption so it can be read and plays quiet stretches at 3x (marked in the
corner).

Needs Pillow and ffmpeg:  python3 scripts/render_demo.py [-o docs/demo.mp4]
"""

import argparse
import bisect
import json
import subprocess
import textwrap

from PIL import Image, ImageDraw, ImageFont

W, H, FPS = 1280, 720, 30
TOTAL = 90.0
TITLE, END = 6.0, 11.0

# Colours: the README chart palette.
BG = (252, 252, 251)
INK = (11, 11, 11)
INK2 = (82, 81, 78)
GRID = (228, 227, 223)
OLD = (214, 213, 208)      # serving the last known good file
NEW = (42, 120, 214)       # serving the new file
FAIL = (214, 69, 65)       # crashed, restarting or not up
TERM_BG = (22, 22, 24)
TERM_INK = (220, 220, 214)
TERM_DIM = (140, 140, 134)
TERM_CMD = (255, 255, 255)
TERM_RED = (255, 122, 110)
TERM_GREEN = (88, 214, 141)

FONT_DIR = "/usr/share/fonts/opentype/inter/"
MONO = "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf"


def font(name, size):
    try:
        return ImageFont.truetype(FONT_DIR + name, size)
    except OSError:
        return ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", size)


F_CAP = font("Inter-SemiBold.otf", 25)
F_LABEL = font("Inter-SemiBold.otf", 17)
F_SMALL = font("Inter-Regular.otf", 15)
F_TILE = font("Inter-Medium.otf", 12)
F_TITLE = font("Inter-Bold.otf", 54)
F_SUB = font("Inter-Regular.otf", 26)
F_BIG = font("Inter-Bold.otf", 40)
F_MONO = ImageFont.truetype(MONO, 14)
MONO_W = F_MONO.getlength("M")


def load(path):
    fleet, events = [], []
    for line in open(path):
        r = json.loads(line)
        (fleet if r["kind"] == "fleet" else events).append(r)
    return fleet, events


def segments(events, end):
    """Video-time to real-time mapping as (v0, v1, r0, r1) pieces."""
    marks = [e["t"] for e in events]
    caps = {e["t"]: e for e in events if e["kind"] == "caption"}
    pieces, v, r = [], 0.0, 0.0

    def play(r1, speed=1.0):
        nonlocal v, r
        if r1 <= r:
            return
        d = (r1 - r) / speed
        pieces.append((v, v + d, r, r1, speed))
        v, r = v + d, r1

    for i, t in enumerate(marks):
        # Fast-forward quiet stretches between events.
        prev = marks[i - 1] if i else 0.0
        if t - prev > 5.0:
            play(prev + 2.5)
            play(t - 1.0, 3.0)
        play(t)
        if t in caps:
            words = len(caps[t]["text"].split())
            hold = min(5.5, max(2.5, words * 0.3))
            pieces.append((v, v + hold, r, r, 0.0))
            v += hold
            del caps[t]
    play(end)
    return pieces


def real_time(pieces, vt):
    for v0, v1, r0, r1, speed in pieces:
        if vt < v1:
            if speed == 0:
                return r0, 0.0
            return r0 + (vt - v0) * speed, speed
    last = pieces[-1]
    return last[3], 1.0


def wrap_px(text, fnt, width):
    words, lines, cur = text.split(), [], ""
    for w in words:
        trial = (cur + " " + w).strip()
        if fnt.getlength(trial) <= width:
            cur = trial
        else:
            lines.append(cur)
            cur = w
    if cur:
        lines.append(cur)
    return lines


def term_lines(events, rt, cols):
    out = []
    for e in events:
        if e["t"] > rt:
            break
        if e["kind"] == "cmd":
            if out:
                out.append(("", TERM_INK))
            out.append(("$ " + e["text"], TERM_CMD))
        elif e["kind"] == "out":
            text = e["text"]
            # The summary after each push repeats the timeline line above it.
            if "record: state/" in text or text.startswith(("  gate check failed", "  crash alarm")):
                continue
            colour = TERM_INK
            if "validate_fail" in text or "gate check failed" in text or "rejected" in text:
                colour = TERM_GREEN
            elif "alarm" in text or "rolled_back" in text:
                colour = TERM_RED
            elif text.startswith("    "):
                colour = TERM_DIM
            for piece in textwrap.wrap(text, cols, subsequent_indent="      ") or [""]:
                out.append((piece, colour))
    return out


def draw_grid(d, insts, x, y, cols, cand, tile=40, gap=6):
    for i, inst in enumerate(insts):
        cx, cy = x + (i % cols) * (tile + gap), y + (i // cols) * (tile + gap)
        up = inst["state"] == "up"
        fill = FAIL if not up else (NEW if inst.get("config_version") == cand else OLD)
        d.rounded_rectangle([cx, cy, cx + tile, cy + tile], radius=5, fill=fill)
        label = f"v{inst['version']}"
        tw = F_TILE.getlength(label)
        d.text((cx + (tile - tw) / 2, cy + tile / 2 - 8), label, font=F_TILE,
               fill=(255, 255, 255) if fill != OLD else INK2)
    rows = (len(insts) + cols - 1) // cols
    return y + rows * (tile + gap)


def counts(insts, cand):
    fail = sum(1 for i in insts if i["state"] != "up")
    new = sum(1 for i in insts if i["state"] == "up" and i.get("config_version") == cand)
    return fail, new


def title_frame():
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    d.text((90, 240), "PushGate", font=F_TITLE, fill=INK)
    d.text((90, 320), "Stop a bad config push before it takes down the fleet.", font=F_SUB, fill=INK2)
    d.text((90, 400), "A live run on a simulated 100-instance fleet: two files rebuilt from", font=F_SMALL, fill=INK2)
    d.text((90, 422), "the Cloudflare (Nov 2025) and Google Cloud (Jun 2025) outages.", font=F_SMALL, fill=INK2)
    return im


def end_frame():
    im = Image.new("RGB", (W, H), BG)
    d = ImageDraw.Draw(im)
    d.text((90, 110), "PushBench: bad pushes held before any instance got the file (10 bad files x 5 fleet layouts)", font=F_LABEL, fill=INK2)
    rows = [("B0  push to all", 5, (42, 120, 214)), ("B2  staged rollout + alarms", 10, (235, 104, 52)),
            ("B3  PushGate", 46, (27, 175, 122))]
    y = 160
    for name, held, colour in rows:
        d.text((90, y + 6), name, font=F_LABEL, fill=INK)
        bw = int(480 * held / 50)
        d.rounded_rectangle([420, y, 420 + max(bw, 6), y + 34], radius=4, fill=colour)
        d.text((430 + bw + 8, y + 4), f"{held} of 50", font=F_LABEL, fill=INK)
        y += 62
    d.text((90, y + 30), "0 of 500 safe files held.  About 0.1 s and 0.13 CPU-seconds per check.", font=F_LABEL, fill=INK)
    d.text((90, y + 62), "Missed: a crash that only a rare request triggers, unless the traffic sample has one.", font=F_SMALL, fill=INK2)
    d.text((90, 600), "github.com/sindhuja-tiwari/PushGate", font=F_BIG, fill=INK)
    return im


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-i", default="state/demo/record.jsonl")
    ap.add_argument("-o", default="docs/demo.mp4")
    args = ap.parse_args()

    fleet, events = load(args.i)
    ftimes = [f["t"] for f in fleet]
    cands = {
        "bot-proxy": json.load(open("gate/testdata/feature-dup.json"))["config_version"],
        "go-policy": json.load(open("gate/testdata/policy-blank.json"))["config_version"],
    }
    end = max(fleet[-1]["t"], events[-1]["t"]) + 0.5
    pieces = segments(events, end)
    play_len = pieces[-1][1]
    end_len = max(4.0, TOTAL - TITLE - play_len)
    print(f"playback {play_len:.1f} s, end card {end_len:.1f} s, total {TITLE + play_len + end_len:.1f} s")

    enc = subprocess.Popen(
        ["ffmpeg", "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", f"{W}x{H}",
         "-r", str(FPS), "-i", "-", "-c:v", "libx264", "-preset", "slow", "-crf", "24",
         "-pix_fmt", "yuv420p", "-movflags", "+faststart", args.o],
        stdin=subprocess.PIPE)

    def emit(im, n):
        raw = im.tobytes()
        for _ in range(n):
            enc.stdin.write(raw)

    emit(title_frame(), int(TITLE * FPS))

    cols = int((W - 40 - 600 - 28) // MONO_W)
    caps = [e for e in events if e["kind"] == "caption"]
    for n in range(int(play_len * FPS)):
        rt, speed = real_time(pieces, n / FPS)
        snap = fleet[max(0, bisect.bisect_right(ftimes, rt) - 1)]["instances"]
        bp = sorted([i for i in snap if i["consumer"] == "bot-proxy"], key=lambda i: i["id"])
        gp = sorted([i for i in snap if i["consumer"] == "go-policy"], key=lambda i: i["id"])

        im = Image.new("RGB", (W, H), BG)
        d = ImageDraw.Draw(im)

        cap = [c for c in caps if c["t"] <= rt]
        if cap:
            for k, line in enumerate(wrap_px(cap[-1]["text"], F_CAP, W - 80)[:2]):
                d.text((40, 22 + k * 34), line, font=F_CAP, fill=INK)
        d.line([40, 96, W - 40, 96], fill=GRID, width=1)

        x0 = 40
        fail, new = counts(bp, cands["bot-proxy"])
        d.text((x0, 112), f"bot-proxy  ·  {len(bp)} instances", font=F_LABEL, fill=INK)
        d.text((x0, 134), f"{new} on the new file   {fail} failing", font=F_SMALL, fill=FAIL if fail else INK2)
        y = draw_grid(d, bp, x0, 160, 10, cands["bot-proxy"], tile=36, gap=5)
        fail, new = counts(gp, cands["go-policy"])
        d.text((x0, y + 10), f"go-policy  ·  {len(gp)} instances", font=F_LABEL, fill=INK)
        d.text((x0, y + 32), f"{new} on the new file   {fail} failing", font=F_SMALL, fill=FAIL if fail else INK2)
        y = draw_grid(d, gp, x0, y + 58, 10, cands["go-policy"], tile=36, gap=5)

        lx = x0
        for colour, label in ((OLD, "last known good file"), (NEW, "new file"), (FAIL, "crashed or restarting")):
            d.rounded_rectangle([lx, y + 12, lx + 14, y + 26], radius=3, fill=colour)
            d.text((lx + 20, y + 10), label, font=F_SMALL, fill=INK2)
            lx += 30 + F_SMALL.getlength(label)

        tx0, ty0, tx1, ty1 = 600, 112, W - 40, H - 46
        d.rounded_rectangle([tx0, ty0, tx1, ty1], radius=8, fill=TERM_BG)
        lines = term_lines(events, rt, cols)
        max_lines = (ty1 - ty0 - 24) // 19
        for k, (text, colour) in enumerate(lines[-max_lines:]):
            d.text((tx0 + 12, ty0 + 12 + k * 19), text, font=F_MONO, fill=colour)

        d.text((40, H - 34), f"run time {rt:5.1f} s", font=F_SMALL, fill=INK2)
        tag = {0.0: "paused to read", 3.0: "3x speed"}.get(speed, "")
        if tag:
            d.text((170, H - 34), tag, font=F_SMALL, fill=INK2)
        url = "github.com/sindhuja-tiwari/PushGate"
        d.text((W - 40 - F_SMALL.getlength(url), H - 34), url, font=F_SMALL, fill=INK2)
        emit(im, 1)

    emit(end_frame(), int(end_len * FPS))
    enc.stdin.close()
    enc.wait()
    print("wrote", args.o)


if __name__ == "__main__":
    main()
