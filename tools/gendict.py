#!/usr/bin/env python3
"""Генерирует встроенные словари из частотных списков FrequencyWords (MIT).

Использование:
  python3 tools/gendict.py <путь к FrequencyWords/content/2018> internal/lang/data

Формат выхода: gzip, строки "слово<TAB>оценка", оценка = log10 частоты на миллиард
слов (как zipf), одна десятичная. Отсортировано по убыванию частоты.
"""
import gzip
import math
import re
import struct
import sys
from pathlib import Path

SPECS = {
    # язык: (файл, регулярка допустимого слова, мин. число вхождений, максимум слов)
    "ru": ("ru/ru_full.txt", re.compile(r"^[а-яё]+(-[а-яё]+)?$"), 8, 150000),
    "en": ("en/en_full.txt", re.compile(r"^[a-z]+('[a-z]+)?$"), 15, 90000),
}


EN_KEYS = "qwertyuiop[]asdfghjkl;'zxcvbnm,.`"
RU_KEYS = "йцукенгшщзхъфывапролджэячсмитьбюё"
CONV = {"en": dict(zip(EN_KEYS, RU_KEYS)), "ru": dict(zip(RU_KEYS, EN_KEYS))}


def clean(dicts):
    """Субтитры содержат «слова», набранные не в той раскладке (lf = да, ещ = to).
    Короткое слово убираем, если его двойник в другом языке намного частотнее."""
    drops = {}
    for lang, other in (("ru", "en"), ("en", "ru")):
        d, o = dicts[lang], dicts[other]
        drop = set()
        for w, z in d.items():
            if len(w) > 3:
                continue
            t = "".join(CONV[lang].get(c, "?") for c in w)
            zt = o.get(t)
            if zt is None:
                continue
            margin = zt - z
            if margin >= 2.5 or (z <= 3.0 and margin >= 1.5):
                drop.add(w)
        drops[lang] = drop
    for lang, drop in drops.items():
        for w in drop:
            del dicts[lang][w]
        print(lang, "removed", len(drop), "layout typos")


def fnv64(b: bytes) -> int:
    h = 14695981039346656037
    for x in b:
        h ^= x
        h = (h * 1099511628211) & 0xFFFFFFFFFFFFFFFF
    return h


def write_known(path: Path, words) -> None:
    """Фильтр Блума «такое слово существует» (ложные срабатывания ~1%).
    Формат: b"EKB1", uint32 число бит, uint32 число хешей, биты (little-endian)."""
    n = max(1, len(words))
    m = int(n * 9.6)
    m = (m + 63) // 64 * 64
    k = 7
    bits = bytearray(m // 8)
    for w in words:
        h1 = fnv64(w.encode("utf-8"))
        h2 = ((h1 >> 33) | 1) & 0xFFFFFFFFFFFFFFFF
        for i in range(k):
            idx = (h1 + i * h2) % m
            bits[idx >> 3] |= 1 << (idx & 7)
    with open(path, "wb") as f:
        f.write(b"EKB1" + struct.pack("<II", m, k) + bytes(bits))
    print(path, len(words), "known forms,", path.stat().st_size, "bytes")


def main() -> None:
    src = Path(sys.argv[1])
    dst = Path(sys.argv[2])
    dst.mkdir(parents=True, exist_ok=True)
    dicts = {}
    for lang, (fname, rx, mincount, maxwords) in SPECS.items():
        rows = []
        total = 0
        with open(src / fname, encoding="utf-8") as f:
            for line in f:
                parts = line.split()
                if len(parts) != 2:
                    continue
                w, c = parts[0].lower(), int(parts[1])
                total += c
                if c < mincount or not rx.match(w):
                    continue
                if lang == "en" and len(w) == 1 and w not in ("a", "i"):
                    continue
                if lang == "ru" and len(w) == 1 and w not in "явксуоибжа":
                    continue
                rows.append((w, c))
        # Все формы, встретившиеся хотя бы дважды: чтобы не «исправлять» редкие, но правильные слова.
        known = set()
        with open(src / fname, encoding="utf-8") as f:
            for line in f:
                parts = line.split()
                if len(parts) == 2 and int(parts[1]) >= 2:
                    w = parts[0].lower()
                    if rx.match(w):
                        known.add(w)
        write_known(dst / f"{lang}.known", known)
        seen = {}
        for w, c in rows:
            seen[w] = seen.get(w, 0) + c
        items = sorted(seen.items(), key=lambda x: -x[1])[:maxwords]
        dicts[lang] = {w: round(math.log10(c / total * 1e9), 1) for w, c in items}
    clean(dicts)
    for lang, d in dicts.items():
        out = dst / f"{lang}.txt.gz"
        with gzip.open(out, "wt", encoding="utf-8", compresslevel=9) as g:
            for w, z in sorted(d.items(), key=lambda x: -x[1]):
                g.write(f"{w}\t{z:.1f}\n")
        print(lang, len(d), "words ->", out, out.stat().st_size, "bytes")


if __name__ == "__main__":
    main()
