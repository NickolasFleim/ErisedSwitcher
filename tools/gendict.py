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
    # Украинский: без ё ы ъ э (так отсекаются русские слова в субтитрах).
    "uk": ("uk/uk_full.txt", re.compile(r"^[а-щьюяєіїґ]+([-'][а-щьюяєіїґ]+)?$"), 3, 120000),
}

# Слова с апострофом в корпусе разрезаны токенизатором — добавляем частые вручную (zipf ~ частотность).
UK_APOSTROPHE = {
    "п'ять": 4.6, "ім'я": 4.6, "сім'я": 4.3, "сім'ї": 4.1, "пам'ять": 4.0, "пам'яті": 3.8, "дев'ять": 4.0,
    "м'ясо": 3.6, "м'який": 3.4, "м'яко": 3.3, "м'яч": 3.3, "об'єкт": 3.4, "об'єкти": 3.0, "з'явився": 3.6,
    "з'явилася": 3.4, "з'явилися": 3.3, "з'ясувати": 3.5, "з'ясуємо": 3.0, "комп'ютер": 3.8, "комп'ютера": 3.4,
    "п'ятниця": 3.4, "п'ятницю": 3.2, "п'ятдесят": 3.6, "п'яний": 3.5, "п'яна": 3.2, "п'ю": 3.3, "п'єш": 3.0,
    "з'їсти": 3.3, "з'їв": 3.2, "під'їзд": 3.0, "від'їзд": 2.8, "об'єднання": 3.2, "об'єм": 2.9, "бур'ян": 2.6,
    "з'єднання": 3.0, "зв'язок": 3.7, "зв'язку": 3.6, "зв'язатися": 3.2, "здоров'я": 3.9, "м'ята": 2.7,
    "п'єса": 2.9, "в'язниця": 3.2, "в'язниці": 3.2, "дзвін": 3.0, "ім'ям": 3.4, "пір'я": 2.8, "сім'ю": 3.8,
    "сім'єю": 3.4, "п'ятеро": 3.2, "дев'яносто": 3.3, "п'ятий": 3.4, "дев'ятий": 3.0, "м'язи": 3.0,
    "з'їздити": 2.8, "об'їхати": 2.7, "роз'яснити": 2.8, "солов'ї": 2.5, "кав'ярня": 3.0, "інтерв'ю": 3.4,
    "прем'єр": 3.0, "прем'єра": 3.0, "з'являється": 3.3, "м'ясом": 2.9,
}


EN_KEYS = "qwertyuiop[]asdfghjkl;'zxcvbnm,.`"
RU_KEYS = "йцукенгшщзхъфывапролджэячсмитьбюё"
UK_KEYS = "йцукенгшщзхїфівапролджєячсмитьбю'"
CONV = {"en": dict(zip(EN_KEYS, RU_KEYS)), "ru": dict(zip(RU_KEYS, EN_KEYS)), "uk": dict(zip(UK_KEYS, EN_KEYS))}
CONV_EN_UK = dict(zip(EN_KEYS, UK_KEYS))


def clean(dicts):
    """Субтитры содержат «слова», набранные не в той раскладке (lf = да, ещ = to).
    Короткое слово убираем, если его двойник в другом языке намного частотнее."""
    drops = {"ru": set(), "en": set(), "uk": set()}
    for lang, other in (("ru", "en"), ("en", "ru"), ("uk", "en"), ("en", "uk")):
        d, o = dicts[lang], dicts[other]
        drop = drops[lang]
        conv = CONV_EN_UK if (lang, other) == ("en", "uk") else CONV[lang]
        for w, z in d.items():
            if len(w) > 3:
                continue
            t = "".join(conv.get(c, "?") for c in w)
            zt = o.get(t)
            if zt is None:
                continue
            margin = zt - z
            if margin >= 2.5 or (z <= 3.0 and margin >= 1.5):
                drop.add(w)
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
        seen = {}
        for w, c in rows:
            seen[w] = seen.get(w, 0) + c
        items = sorted(seen.items(), key=lambda x: -x[1])[:maxwords]
        dicts[lang] = {w: round(math.log10(c / total * 1e9), 1) for w, c in items}
        if lang == "uk":
            for w, z in UK_APOSTROPHE.items():
                dicts[lang].setdefault(w, z)
                known.add(w)
        write_known(dst / f"{lang}.known", known)
    clean(dicts)
    for lang, d in dicts.items():
        out = dst / f"{lang}.txt.gz"
        with gzip.open(out, "wt", encoding="utf-8", compresslevel=9) as g:
            for w, z in sorted(d.items(), key=lambda x: -x[1]):
                g.write(f"{w}\t{z:.1f}\n")
        print(lang, len(d), "words ->", out, out.stat().st_size, "bytes")


if __name__ == "__main__":
    main()
