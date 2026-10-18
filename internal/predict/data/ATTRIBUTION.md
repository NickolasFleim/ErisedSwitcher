# Стартовая база пар слов

Файлы `*.pairs.gz` — счётчики пар соседних слов («слово → следующее слово»), посчитанные
скриптом `tools/genpairs.py` по корпусам [Universal Dependencies](https://universaldependencies.org/):

| Файл | Корпус | Лицензия |
|---|---|---|
| `en.pairs.gz` | [UD_English-EWT](https://github.com/UniversalDependencies/UD_English-EWT) (English Web Treebank) | CC BY-SA 4.0 |
| `ru.pairs.gz` | [UD_Russian-Taiga](https://github.com/UniversalDependencies/UD_Russian-Taiga), [UD_Russian-GSD](https://github.com/UniversalDependencies/UD_Russian-GSD) | CC BY-SA 4.0 |
| `uk.pairs.gz` | [UD_Ukrainian-ParlaMint](https://github.com/UniversalDependencies/UD_Ukrainian-ParlaMint) | CC BY-SA 4.0 |

Сами тексты в программу не входят — только счётчики пар. Эти данные распространяются на условиях
[CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/) (совместима с GNU GPL v3).
Корпуса с запретом коммерческого использования (NC) не использовались.
