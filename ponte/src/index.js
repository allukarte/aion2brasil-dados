// Ponte do AION 2 Brasil: deixa o blog consultar a busca e a ficha públicas do site oficial (servidor LatAm, em português).
// Só lê; não guarda nada além de um cache curto.
//   GET /busca?nome=Myrae[&servidor=1401]          -> lista de personagens
//   GET /ficha?servidor=1401&id=<characterId>       -> perfil, atributos e equipamento
const SITE = "https://aion2.plaync.com";
const BUSCA = "https://api-search.plaync.com/aion2global/search/v2/character";
const BASE = { lang: "pt-BR", region: "la" };
const ORIGENS = [/^https:\/\/www\.aion2brasil\.com\.br$/, /^https:\/\/aion2brasil\.com\.br$/, /^https:\/\/[a-z0-9-]+\.blogspot\.com$/, /^https:\/\/www\.blogger\.com$/, /^https:\/\/[a-z0-9-]+\.googleusercontent\.com$/];
const UA = "aion2brasil-ponte (+https://www.aion2brasil.com.br)";

function cors(origem) {
  const ok = origem && ORIGENS.some((r) => r.test(origem));
  return {
    "Access-Control-Allow-Origin": ok ? origem : "https://www.aion2brasil.com.br",
    "Access-Control-Allow-Methods": "GET, OPTIONS",
    "Vary": "Origin",
  };
}
function resposta(dados, status, origem, segundos) {
  return new Response(JSON.stringify(dados), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": `public, max-age=${segundos || 0}`, ...cors(origem) },
  });
}
async function oficial(url) {
  const r = await fetch(url, { headers: { "User-Agent": UA, Accept: "application/json" }, cf: { cacheTtl: 300 } });
  if (!r.ok) throw new Error("site oficial respondeu " + r.status);
  return r.json();
}
const CLASSE = { GLADIATOR: "Gladiador", TEMPLAR: "Templário", ASSASSIN: "Assassino", RANGER: "Arqueiro", SORCERER: "Feiticeiro", ELEMENTALIST: "Espiritualista", CLERIC: "Clérigo", CHANTER: "Encantador" };
let PC = null;
async function classes() {
  if (PC) return PC;
  try {
    const d = await fetch(SITE + "/pt-br/api/gameinfo/pcdata?" + qs(BASE), { headers: { "User-Agent": UA, Accept: "application/json" }, cf: { cacheTtl: 86400 } }).then((r) => r.json());
    PC = {};
    (d.pcDataList || []).forEach((p) => { PC[p.id] = CLASSE[String(p.className).toUpperCase()] || p.className; });
  } catch (e) { PC = {}; }
  return PC;
}
function limpa(t) { return String(t || "").replace(/<[^>]*>/g, ""); }
function qs(o) { return new URLSearchParams(o).toString(); }

export default {
  async fetch(req, env, ctx) {
    const origem = req.headers.get("Origin");
    if (req.method === "OPTIONS") return new Response(null, { headers: cors(origem) });
    if (req.method !== "GET") return resposta({ erro: "só GET" }, 405, origem);
    const u = new URL(req.url);
    const cache = caches.default;
    const chave = new Request(u.origin + u.pathname + "?" + [...u.searchParams].sort().map(([k, v]) => k + "=" + encodeURIComponent(v)).join("&"));
    const guardado = await cache.match(chave);
    if (guardado) {
      const h = new Headers(guardado.headers);
      Object.entries(cors(origem)).forEach(([k, v]) => h.set(k, v));
      return new Response(guardado.body, { status: guardado.status, headers: h });
    }
    let saida;
    try {
      if (u.pathname === "/busca") {
        const nome = (u.searchParams.get("nome") || "").trim();
        if (nome.length < 2 || nome.length > 20) return resposta({ erro: "nome com 2 a 20 letras" }, 400, origem);
        const p = { keyword: nome, page: "1", size: "30", region: BASE.region, localeInfo: BASE.lang };
        const sv = u.searchParams.get("servidor");
        if (sv && /^\d{4}$/.test(sv)) p.serverId = sv;
        const [d, pc] = await Promise.all([oficial(BUSCA + "?" + qs(p)), classes()]);
        saida = resposta({ total: d.pagination ? d.pagination.total : 0, lista: (d.list || []).map((x) => ({
          id: decodeURIComponent(x.characterId || ""), nome: limpa(x.name), servidor: x.serverId, nomeServidor: limpa(x.serverName),
          classe: pc[x.pcId] || "", pcId: x.pcId, raca: x.race, nivel: x.level,
          foto: x.profileImageUrl ? "https://profileimg.plaync.com" + x.profileImageUrl : "",
        })) }, 200, origem, 600);
      } else if (u.pathname === "/ficha") {
        const sv = u.searchParams.get("servidor"), id = u.searchParams.get("id") || "";
        if (!/^\d{4}$/.test(sv || "") || !/^[A-Za-z0-9_\-=]{10,80}$/.test(id)) return resposta({ erro: "servidor e id" }, 400, origem);
        const p = qs({ ...BASE, serverId: sv, characterId: id });
        const [info, equip] = await Promise.all([oficial(SITE + "/api/character/info?" + p), oficial(SITE + "/api/character/equipment?" + p).catch(() => null)]);
        if (!info.profile || !info.profile.characterId) return resposta({ erro: "personagem não encontrado" }, 404, origem, 60);
        saida = resposta({ info, equip }, 200, origem, 600);
      } else if (u.pathname === "/daevanion") {
        // nós abertos dos 6 tabuleiros de Daevanion: { "1": [[linha, coluna], ...], ... }
        const sv = u.searchParams.get("servidor"), id = u.searchParams.get("id") || "", cl = +(u.searchParams.get("classe") || 0);
        if (!/^\d{4}$/.test(sv || "") || !/^[A-Za-z0-9_\-=]{10,80}$/.test(id) || cl < 2 || cl > 9) return resposta({ erro: "servidor, id e classe" }, 400, origem);
        const tab = {};
        await Promise.all([1, 2, 3, 4, 5, 6].map(async (pos) => {
          try {
            const d = await oficial(SITE + "/api/character/daevanion/detail?" + qs({ ...BASE, serverId: sv, characterId: id, boardId: String((cl - 1) * 10 + pos) }));
            tab[pos] = (d.nodeList || []).filter((n) => n.open === 1 || n.open === true || n.open === "1").map((n) => [n.row, n.col]);
          } catch (e) { tab[pos] = null; }
        }));
        saida = resposta({ tabuleiros: tab }, 200, origem, 300);
      } else {
        return resposta({ ok: true, uso: ["/busca?nome=", "/ficha?servidor=&id=", "/daevanion?servidor=&id=&classe="] }, 200, origem, 3600);
      }
    } catch (e) {
      return resposta({ erro: String(e.message || e) }, 502, origem);
    }
    ctx.waitUntil(cache.put(chave, saida.clone()));
    return saida;
  },
};
