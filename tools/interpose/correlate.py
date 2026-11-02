#!/usr/bin/env python3
"""Resume work/logs/xu.log: marcas (MARK), escrituras (OUT) y lecturas XU que cambian.

Uso: tools/interpose/correlate.py [work/logs/xu.log] [--all]
Quita el ruido de sondeo (la app lee sel 0x02 y 0x1c de la unit 9 cada ~1 s) salvo que cambie
el valor, y muestra por unit/selector: tiempo relativo, direccion, longitud y datos.
"""
import re, sys

path = next((a for a in sys.argv[1:] if not a.startswith("--")), "work/logs/xu.log")
show_all = "--all" in sys.argv
last = {}
t0 = None
for line in open(path, errors="replace"):
    parts = line.rstrip("\n").split(" ", 2)
    if len(parts) < 3:
        continue
    ts, kind, rest = float(parts[0]), parts[1], parts[2]
    t0 = t0 or ts
    rel = ts - t0
    if kind == "MARK":
        print(f"{rel:9.3f}  ==== MARK {rest} ====")
        continue
    if kind not in ("OUT", "IN") or "unit=" not in rest:
        continue
    m = re.search(r"unit=(\d+) sel=(0x[0-9a-f]+)", rest)
    req = re.search(r"req=([0-9a-f]+)", rest).group(1)
    data = re.search(r"data=([0-9a-f]+)", rest)
    data = data.group(1) if data else ""
    key = (kind, m.group(1), m.group(2), req)
    if kind == "IN" and req != "81" and not show_all:
        continue  # GET_LEN/MIN/MAX... son ruido
    if kind == "IN" and not show_all and last.get(key) == data:
        continue
    last[key] = data
    d = data if len(data) <= 120 else data[:120] + f"...({len(data)//2}B)"
    print(f"{rel:9.3f}  {kind:3} unit={m.group(1):>2} sel={m.group(2)}  {d}")
