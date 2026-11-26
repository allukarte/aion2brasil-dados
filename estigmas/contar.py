"""Conta as Estigmas equipadas pelos personagens com mais CP de cada classe no LatAm.

Lê a lista de personagens do robô (estado/*.json.gz), pega os TOP com mais CP de cada classe
(nível 45, sem CP suspeito), abre a ficha pública oficial de cada um (a mesma que a Busca de
Personagem do site mostra) e conta as Estigmas equipadas. Não guarda nome nem id de ninguém:
só a contagem por classe.

Uso: python3 estigmas/contar.py [TOP]   (padrão: 100 por classe)
Saída: dados/estigmas-AAAA-MM-DD.json e estigmas-resumo.md
"""
import collections, datetime, glob, gzip, json, sys, time, urllib.parse, urllib.request

TOP = int(sys.argv[1]) if len(sys.argv) > 1 else 100
SITE = "https://aion2.plaync.com/api/character/equipment?"
UA = "robo-aion2brasil (+https://www.aion2brasil.com.br)"
CLASSE = {2: "Gladiador", 3: "Templário", 4: "Arqueiro", 5: "Assassino", 6: "Espiritualista", 7: "Feiticeiro", 8: "Clérigo", 9: "Encantador"}


def suspeito(p, rep):
    if p.get("i"):
        return p["cp"] > p["i"] * 100
    return p["cp"] >= 300000 or (rep[p["cp"]] >= 3 and not p.get("k"))


cand = collections.defaultdict(list)
for f in glob.glob("estado/*.json.gz"):
    S = json.load(gzip.open(f))
    rep = collections.Counter(p.get("cp", 0) for p in S["p"].values() if p.get("cp"))
    for cid, p in S["p"].items():
        if p.get("l", 0) >= 45 and p.get("cp") and p.get("c") in CLASSE and not suspeito(p, rep):
            cand[p["c"]].append((p["cp"], S["id"], cid))


def ficha(sv, cid):
    q = urllib.parse.urlencode({"lang": "pt-BR", "region": "la", "serverId": sv, "characterId": cid})
    for t in range(3):
        try:
            r = urllib.request.urlopen(urllib.request.Request(SITE + q, headers={"User-Agent": UA, "Accept": "application/json"}), timeout=30)
            return json.load(r)
        except Exception as e:
            time.sleep(5 * (t + 1))
    return None


saida = {"data": datetime.date.today().isoformat(), "top": TOP, "classes": {}}
md = ["# Estigmas dos %d personagens com mais CP de cada classe (LatAm, %s)" % (TOP, saida["data"]), ""]
for c, nome in CLASSE.items():
    L = sorted(cand[c], reverse=True)[:TOP]
    uso, qtd, combos, lidos = collections.Counter(), collections.Counter(), collections.Counter(), 0
    for cp, sv, cid in L:
        d = ficha(sv, cid)
        time.sleep(1)
        if not d:
            continue
        sk = [k for k in ((d.get("skill") or {}).get("skillList") or []) if k.get("category") == "Dp" and k.get("skillLevel", 0) > 0 and k.get("equip")]
        lidos += 1
        qtd[len(sk)] += 1
        nomes = sorted(k["name"] for k in sk)
        uso.update(nomes)
        combos[" + ".join(nomes)] += 1
    saida["classes"][nome] = {"lidos": lidos, "cp_min": L[-1][0] if L else 0, "quantas_equipadas": dict(sorted(qtd.items())),
                              "uso": uso.most_common(), "combinacoes": combos.most_common(8)}
    md += ["## %s (%d fichas, CP a partir de %s)" % (nome, lidos, L[-1][0] if L else 0),
           "Quantas Estigmas equipadas: " + ", ".join("%d → %d" % kv for kv in sorted(qtd.items())), ""]
    md += ["- %s: %d (%d%%)" % (n, v, round(100 * v / max(1, lidos))) for n, v in uso.most_common()]
    md += ["", "Combinações mais comuns:"] + ["- %d× %s" % (v, k) for k, v in combos.most_common(5)] + [""]
    print(nome, lidos, dict(qtd), flush=True)

json.dump(saida, open("dados/estigmas-%s.json" % saida["data"], "w", encoding="utf-8"), ensure_ascii=False, indent=1)
open("estigmas-resumo.md", "w", encoding="utf-8").write("\n".join(md))
