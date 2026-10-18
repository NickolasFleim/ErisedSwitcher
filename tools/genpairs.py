#!/usr/bin/env python3
"""Стартовая база «следующего слова»: частые пары соседних слов из корпусов Universal Dependencies.

    python3 tools/genpairs.py <папка с клонами UD_*> internal/predict/data

Берутся только корпуса под CC BY-SA 4.0 (совместима с GPL-3.0). Пара учитывается, только если
слова стоят рядом внутри предложения без знака препинания между ними и оба есть в словаре программы.
Формат: gzip, строки «слово<TAB>следующее<TAB>счётчик».
"""
import collections
import glob
import gzip
import sys
from pathlib import Path

CORPORA = {
    "en": ["UD_English-EWT"],
    "ru": ["UD_Russian-Taiga", "UD_Russian-GSD"],
    "uk": ["UD_Ukrainian-ParlaMint"],
}
LIMIT = {"en": 25000, "ru": 40000, "uk": 15000}
MIN_COUNT = {"en": 3, "ru": 3, "uk": 2}


def dictionary(lang):
    words = set()
    with gzip.open(f"internal/lang/data/{lang}.txt.gz", "rt", encoding="utf-8") as f:
        for line in f:
            words.add(line.split("\t", 1)[0])
    return words


def sentences(path):
    toks = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line:
                if toks:
                    yield toks
                toks = []
                continue
            if line.startswith("#"):
                continue
            cols = line.split("\t")
            if len(cols) < 4 or "-" in cols[0] or "." in cols[0]:
                continue  # составные токены и пустые узлы
            toks.append((cols[1], cols[3]))
    if toks:
        yield toks


def main():
    src, dst = Path(sys.argv[1]), Path(sys.argv[2])
    dst.mkdir(parents=True, exist_ok=True)
    for lang, repos in CORPORA.items():
        known = dictionary(lang)
        pairs = collections.Counter()
        nsent = 0
        for repo in repos:
            for path in sorted(glob.glob(str(src / repo / "*.conllu"))):
                for toks in sentences(path):
                    nsent += 1
                    prev = None
                    for form, upos in toks:
                        w = form.lower().replace("’", "'").replace("ʼ", "'")
                        if upos == "PUNCT" or w not in known:
                            prev = None
                            continue
                        if prev:
                            pairs[(prev, w)] += 1
                        prev = w
        top = [(p, c) for p, c in pairs.most_common() if c >= MIN_COUNT[lang]][: LIMIT[lang]]
        out = dst / f"{lang}.pairs.gz"
        with gzip.open(out, "wt", encoding="utf-8", compresslevel=9) as g:
            for (a, b), c in top:
                g.write(f"{a}\t{b}\t{c}\n")
        print(lang, nsent, "sentences,", len(pairs), "pairs ->", len(top), "kept,", out.stat().st_size, "bytes")


if __name__ == "__main__":
    main()
