"""Robô de manutenção do aion2brasil.com.br

Lê todas as páginas do blog pelo sitemap e procura:
  1. links quebrados (páginas do próprio blog e sites de fora) e imagens que não abrem;
  2. nomes antigos que voltaram a aparecer (compara com a última vez: só avisa o que é novo);
  3. páginas com "Próxima atualização" numa data que já passou.
Grava manutencao/relatorio.md e manutencao/base.json, e atualiza a issue "Relatório de manutenção".
"""
import concurrent.futures as cf
import datetime as dt
import html
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

SITE = "https://www.aion2brasil.com.br"
AQUI = os.path.dirname(os.path.abspath(__file__))
UA = "Mozilla/5.0 (robo-manutencao aion2brasil; +https://github.com/allukarte/aion2brasil-dados)"
HOJE = dt.date.today()

TERMOS = [
    ("Mago (usar Feiticeiro)", r"\bMagos?\b"),
    ("Chanter/Cantor (usar Encantador)", r"\b(Chanter|Cantor)\b"),
    ("Sorcerer (usar Feiticeiro)", r"\bSorcerer\b"),
    ("Elementalista (usar Espiritualista)", r"\bElementalist[a]?\b"),
    ("Odyle (usar Od)", r"\bOdyle\b|\bOdylium\b"),
    ("Stigma (usar Estigma)", r"\bStigmas?\b"),
    ("Festa Shugo (usar Shugo Festa)", r"Festa Shugo|Shugo Festival|Festival Shugo"),
    ("Pedra de Encantamento (usar Pedra de Aprimoramento)", r"Pedra de Encantamento|Enhance Stone"),
    ("termo velho de atributo", r"Hit Cr[íi]tico|Hit-M[úu]ltiplo|Multi-Hit|\bSmite\b|\bEndurance\b|Chance Perfeita"),
    ("HP/MP (usar PV/PM)", r"\b(HP|MP)\b"),
    ("dado da Coreia/Taiwan", r"\b(KR|TW)\b|Coreia|Taiwan"),
]
TERMOS = [(n, re.compile(p)) for n, p in TERMOS]


def baixar(url, metodo="GET", tempo=25):
    for tentativa in range(4):
        st, b = _baixar(url, metodo, tempo)
        if st != 429:
            return st, b
        time.sleep(20 * (tentativa + 1))  # o Blogger pediu calma
    return st, b


def _baixar(url, metodo="GET", tempo=25):
    req = urllib.request.Request(url, method=metodo, headers={"User-Agent": UA, "Accept-Language": "pt-BR"})
    try:
        with urllib.request.urlopen(req, timeout=tempo) as r:
            return r.status, (r.read() if metodo == "GET" else b"")
    except urllib.error.HTTPError as e:
        return e.code, b""
    except Exception as e:  # DNS, tempo esgotado, certificado...
        return f"erro: {type(e).__name__}", b""


def paginas():
    urls = []
    for mapa in ["/sitemap.xml", "/sitemap-pages.xml"]:
        fila = [SITE + mapa]
        while fila:
            st, b = baixar(fila.pop())
            if st != 200:
                continue
            locs = re.findall(r"<loc>(.*?)</loc>", b.decode("utf-8", "ignore"))
            for l in locs:
                l = html.unescape(l.strip())
                (fila if "sitemap" in l and l.endswith((".xml",)) or "sitemap.xml?page=" in l else urls).append(l)
    return sorted(set(urls))


def corpo(h):
    """Só o texto do post (sem a barra lateral), com os \\uXXXX dos scripts já traduzidos."""
    m = re.search(r"""class=["'][^"']*\bpost-body\b""", h)
    if m:
        f = re.search(r"""class=["'][^"']*\bpost-footer\b""", h[m.end():])
        h = h[m.start(): m.end() + f.start() if f else len(h)]
    h = re.sub(r"\\u([0-9a-fA-F]{4})", lambda m: chr(int(m.group(1), 16)), h)
    return h


def texto(h):
    h = re.sub(r"<style.*?</style>", " ", h, flags=re.S)
    h = re.sub(r"https?://\S+", " ", h)  # nomes dentro de links de imagem não contam
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", " ", h)))


def main():
    base = {}
    try:
        base = json.load(open(os.path.join(AQUI, "base.json")))
    except Exception:
        pass
    primeira = not base
    urls = paginas()
    print("páginas", len(urls))
    if not urls:
        print("sitemap vazio")
        sys.exit(1)

    links = {}  # url -> páginas que usam
    termos = {}  # página -> {termo: quantos}
    datas = []
    falhas = []

    def lidas():
        for u in urls:  # uma por vez: o Blogger devolve 429 se for rápido demais
            yield u, baixar(u)
            time.sleep(1.5)

    if True:
        for u, (st, b) in lidas():
            if st != 200:
                falhas.append((u, st))
                continue
            h = corpo(b.decode("utf-8", "ignore"))
            for l in re.findall(r'(?:href|src)\s*=\s*["\'](https?://[^"\'\s<>]+)', h):
                l = html.unescape(l).split("#")[0]
                if any(x in l for x in ("blogger.com/", "googleusercontent.com/", "fonts.googleapis", "fonts.gstatic", "googletagmanager", "javascript:", "${")):
                    continue
                links.setdefault(l, set()).add(u)
            t = texto(h)
            c = {n: len(p.findall(t)) for n, p in TERMOS}
            termos[u] = {k: v for k, v in c.items() if v}
            for d in re.findall(r"Pr[óo]xima atualiza[çc][ãa]o:?\s*(\d{2}/\d{2}/\d{4})", t):
                try:
                    if dt.datetime.strptime(d, "%d/%m/%Y").date() < HOJE:
                        datas.append((u, d))
                except ValueError:
                    pass

    print("links", len(links))

    conhecidas = {u.split("?")[0].rstrip("/") for u in urls} | {SITE}

    def checar(l):
        if l.split("?")[0].rstrip("/") in conhecidas:
            return l, 200
        if l.startswith(SITE):
            time.sleep(1.5)
        st, _ = baixar(l, "HEAD", 20)
        if st in (403, 405, 400, 501) or isinstance(st, str):
            st, _ = baixar(l, "GET", 25)
        return l, st

    quebrados, bloqueados = [], []
    internos = [l for l in sorted(links) if l.startswith(SITE)]
    externos = [l for l in sorted(links) if not l.startswith(SITE)]
    with cf.ThreadPoolExecutor(6) as ex:
        res = [checar(l) for l in internos] + list(ex.map(checar, externos))
    if True:
        for l, st in res:
            if st in (404, 410) or (isinstance(st, str) and "URLError" in st):
                quebrados.append((l, st))
            elif isinstance(st, str) or st >= 400:
                bloqueados.append((l, st))

    # termos: só avisa o que aumentou desde a última vez
    novos = []
    vistas = set(base.get("paginas", []))
    for u, c in termos.items():
        if u in vistas and u not in base.get("termos", {}):
            continue  # da última vez a página não abriu: vira base agora
        antes = base.get("termos", {}).get(u, {})
        for k, v in c.items():
            if v > antes.get(k, 0):
                novos.append((u, k, v, antes.get(k, 0)))

    curto = lambda u: u.replace(SITE, "")
    L = [f"# Relatório de manutenção · {HOJE.strftime('%d/%m/%Y')}", "",
         f"{len(urls)} páginas lidas, {len(links)} links e imagens conferidos.", ""]
    L.append(f"## Links e imagens quebrados ({len(quebrados)})")
    L += [f"- {l} ({st}) · em: " + ", ".join(curto(p) for p in sorted(links[l])[:5]) for l, st in quebrados] or ["Nenhum."]
    L += ["", f"## Nomes antigos que apareceram desde a última vez ({0 if primeira else len(novos)})"]
    if primeira:
        L.append("Primeira leitura: os nomes que já estavam no blog viraram a base. A partir da próxima, só aparece aqui o que for novo.")
    else:
        L += [f"- {curto(u)}: {k} ({a} → {v})" for u, k, v, a in sorted(novos)] or ["Nenhum."]
    L += ["", f"## \"Próxima atualização\" com data vencida ({len(datas)})"]
    L += [f"- {curto(u)}: dizia {d}" for u, d in datas] or ["Nenhuma."]
    L += ["", f"## Páginas que não abriram ({len(falhas)})"]
    L += [f"- {curto(u)} ({st})" for u, st in falhas] or ["Nenhuma."]
    L += ["", f"<details><summary>Sites de fora que recusaram o robô ({len(bloqueados)}), conferir na mão se quiser</summary>", ""]
    L += [f"- {l} ({st})" for l, st in bloqueados]
    L += ["", "</details>", ""]
    rel = "\n".join(L)
    open(os.path.join(AQUI, "relatorio.md"), "w", encoding="utf-8").write(rel)
    t_novo = dict(base.get("termos", {}))
    t_novo.update(termos)
    base_nova = {"data": HOJE.isoformat(), "paginas": sorted(vistas | set(urls)), "termos": t_novo}
    json.dump(base_nova, open(os.path.join(AQUI, "base.json"), "w", encoding="utf-8"), ensure_ascii=False, indent=0, sort_keys=True)
    problemas = len(quebrados) + (0 if primeira else len(novos)) + len(datas) + len(falhas)
    open(os.path.join(AQUI, "..", "manutencao-resumo.txt"), "w").write(str(problemas))
    print(rel[:3000])
    issue(rel, problemas)


def issue(corpo_md, problemas):
    tok, repo = os.environ.get("GH_TOKEN"), os.environ.get("GITHUB_REPOSITORY")
    if not tok or not repo:
        return
    api = f"https://api.github.com/repos/{repo}"

    def gh(metodo, url, dados=None):
        req = urllib.request.Request(url, method=metodo, data=json.dumps(dados).encode() if dados else None,
                                     headers={"Authorization": f"Bearer {tok}", "Accept": "application/vnd.github+json", "User-Agent": UA})
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read() or b"null")

    titulo = "Relatório de manutenção do blog"
    abertas = gh("GET", f"{api}/issues?state=open&per_page=100") or []
    atual = next((i for i in abertas if i.get("title") == titulo), None)
    if atual:
        gh("PATCH", f"{api}/issues/{atual['number']}", {"body": corpo_md})
        n = atual["number"]
    else:
        n = gh("POST", f"{api}/issues", {"title": titulo, "body": corpo_md})["number"]
    if problemas:
        # comentário novo = aviso por e-mail e no app
        gh("POST", f"{api}/issues/{n}/comments", {"body": f"Relatório da semana: {problemas} coisa(s) para olhar. O relatório completo está na descrição desta issue."})


if __name__ == "__main__":
    main()
