// Robô do AION 2 Brasil: lista os personagens do servidor SA (API do site oficial, via github.com/nuriland/aion2-api),
// consulta o CP aos poucos e gera o ranking de CP e o censo. Uso: go run . [medir|diario]
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	aion2 "github.com/nuriland/aion2-api"
)

const pausa = 0 // o limitador da biblioteca já espaça: no máximo 2,5 pedidos por segundo

var letras = strings.Split("abcdefghijklmnopqrstuvwxyz0123456789", "")

type Pers struct {
	N  string `json:"n"` // nome
	C  int    `json:"c"` // classe
	L  int    `json:"l"` // nível
	R  int    `json:"r"` // raça
	CP int    `json:"cp,omitempty"`
	T  string `json:"t,omitempty"` // título
	G  string `json:"g,omitempty"` // legião
	Q  string `json:"q,omitempty"` // dia da última consulta do CP (AAAA-MM-DD)
	V  string `json:"v"`           // dia em que apareceu na lista
	M  string `json:"m,omitempty"` // último dia em que o CP mudou (sinal de que a pessoa está jogando)
}

type Servidor struct {
	ID    int              `json:"id"`
	Nome  string           `json:"nome"`
	Raca  int              `json:"raca"`
	Lista string           `json:"lista"` // dia da última varredura completa
	Prox  int              `json:"prox"`  // próxima letra da varredura em andamento
	P     map[string]*Pers `json:"p"`     // characterId -> personagem
}

var (
	c      *aion2.Client
	ctx    context.Context
	hoje   = time.Now().UTC().Format("2006-01-02")
	limite time.Time
)

func ler(f string, v any) bool {
	fh, err := os.Open(f)
	if err != nil {
		return false
	}
	defer fh.Close()
	var r io.Reader = fh
	if strings.HasSuffix(f, ".gz") {
		z, err := gzip.NewReader(fh)
		if err != nil {
			return false
		}
		r = z
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}
func gravar(f string, v any, bonito bool) {
	os.MkdirAll(filepath.Dir(f), 0755)
	var b []byte
	if bonito {
		b, _ = json.MarshalIndent(v, "", " ")
	} else {
		b, _ = json.Marshal(v)
	}
	if strings.HasSuffix(f, ".gz") {
		fh, err := os.Create(f)
		if err != nil {
			return
		}
		z := gzip.NewWriter(fh)
		z.Write(b)
		z.Close()
		fh.Close()
		return
	}
	os.WriteFile(f, b, 0644)
}
func tempo() bool { return time.Now().Before(limite) }

func main() {
	modo := "diario"
	if len(os.Args) > 1 {
		modo = os.Args[1]
	}
	min, vm, cm := 300, 90, 0
	if v := os.Getenv("MINUTOS"); v != "" {
		fmt.Sscan(v, &min)
	}
	if v := os.Getenv("VARRER_MIN"); v != "" {
		fmt.Sscan(v, &vm)
	}
	if v := os.Getenv("CP_MIN"); v != "" {
		fmt.Sscan(v, &cm)
	}
	limite = time.Now().Add(time.Duration(min) * time.Minute)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(context.Background(), time.Duration(min+20)*time.Minute)
	defer cancel()
	var err error
	c, err = aion2.New(aion2.ConfigOpts{Region: aion2.RegionSA, Locale: aion2.LocalePTBR, RateLimit: 2.5})
	if err != nil {
		panic(err)
	}
	if modo == "patch" {
		patch()
		return
	}
	sv, err := c.Servers(ctx)
	if err != nil {
		panic(err)
	}
	cl, _ := c.Classes(ctx)
	gravar("dados/servidores.json", map[string]any{"servidores": sv, "classes": cl, "atualizado": hoje}, true)
	if modo == "medir" {
		medir(sv)
		return
	}
	fim := limite
	limite = time.Now().Add(time.Duration(vm) * time.Minute)
	if limite.After(fim) {
		limite = fim
	}
	for _, s := range sv {
		varrer(s)
		if !tempo() {
			break
		}
	}
	limite = time.Now().Add(time.Duration(cm) * time.Minute)
	if cm == 0 || limite.After(fim) {
		limite = fim
	}
	cps(sv)
	saida(sv)
}

func medir(sv []aion2.Server) {
	out := map[string]any{}
	t0 := time.Now()
	n := 0
	for _, s := range sv {
		r, err := c.SearchCharacters(ctx, aion2.CharacterSearch{Keyword: "a", RaceID: s.RaceID, ServerID: s.ServerID, ClassIDs: []int{5}, Size: 200})
		n++
		time.Sleep(pausa)
		if err != nil {
			out[s.Name] = err.Error()
			continue
		}
		out[s.Name] = r.Page.Total
	}
	out["segundos_por_pedido"] = time.Since(t0).Seconds() / float64(n)
	gravar("dados/medir.json", out, true)
	fmt.Println(out)
}

func arq(s aion2.Server) string { return fmt.Sprintf("estado/%d.json.gz", s.ServerID) }

// varrer: busca por letra e classe (a busca acha a letra em qualquer parte do nome), até 200 por página
func varrer(s aion2.Server) {
	var S Servidor
	if !ler(arq(s), &S) || S.P == nil {
		S = Servidor{ID: s.ServerID, Nome: s.Name, Raca: s.RaceID, P: map[string]*Pers{}}
	}
	velha := S.Lista == "" || dias(S.Lista) >= 7
	if !velha && S.Prox == 0 {
		return
	}
	cls, _ := c.Classes(ctx)
	for i := S.Prox; i < len(letras); i++ {
		if !tempo() {
			S.Prox = i
			gravar(arq(s), S, false)
			return
		}
		for _, k := range cls {
			for pg := 1; ; pg++ {
				r, err := c.SearchCharacters(ctx, aion2.CharacterSearch{Keyword: letras[i], RaceID: s.RaceID, ServerID: s.ServerID, ClassIDs: []int{k.ID}, Size: 200, Page: pg})
				time.Sleep(pausa)
				if err != nil {
					fmt.Println("erro", s.Name, letras[i], k.ID, pg, err)
					break
				}
				for _, it := range r.Items {
					p := S.P[it.Ref.CharacterID]
					if p == nil {
						p = &Pers{V: hoje}
						S.P[it.Ref.CharacterID] = p
					}
					p.N, p.C, p.L, p.R = it.Name, it.ClassID, it.Level, it.RaceID
				}
				if pg >= r.Page.LastPage || len(r.Items) == 0 {
					break
				}
			}
		}
	}
	S.Prox = 0
	S.Lista = hoje
	gravar(arq(s), S, false)
	fmt.Println("lista", s.Name, len(S.P))
}

func dias(d string) int {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return 999
	}
	return int(time.Since(t).Hours() / 24)
}

// cps: consulta o CP dos personagens nível 45, os mais antigos primeiro, enquanto houver tempo
func cps(sv []aion2.Server) {
	type alvo struct {
		s  int
		id string
		q  string
	}
	all := map[int]*Servidor{}
	var fila []alvo
	for _, s := range sv {
		var S Servidor
		if !ler(arq(s), &S) {
			continue
		}
		all[s.ServerID] = &S
		for id, p := range S.P {
			if p.L >= 45 && (p.Q == "" || dias(p.Q) >= 6) {
				fila = append(fila, alvo{s.ServerID, id, p.Q})
			}
		}
	}
	sort.Slice(fila, func(i, j int) bool { return fila[i].q < fila[j].q })
	n := 0
	var mu sync.Mutex
	jobs := make(chan alvo)
	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				ch, err := c.Character(ctx, aion2.CharacterRef{ServerID: a.s, CharacterID: a.id})
				mu.Lock()
				p := all[a.s].P[a.id]
				p.Q = hoje
				if err == nil {
					if p.CP > 0 && ch.Profile.CombatPower != p.CP {
						p.M = hoje
					}
					p.CP, p.T, p.G, p.L, p.N = ch.Profile.CombatPower, ch.Profile.TitleName, ch.Profile.GuildName, ch.Profile.Level, ch.Profile.Name
					n++
					if n%2000 == 0 {
						for _, s := range sv {
							if S := all[s.ServerID]; S != nil {
								gravar(arq(s), S, false)
							}
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, a := range fila {
		if !tempo() {
			break
		}
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	for _, s := range sv {
		if S := all[s.ServerID]; S != nil {
			gravar(arq(s), S, false)
		}
	}
	fmt.Println("cp consultados", n, "fila", len(fila))
}

// saida: arquivos pequenos que o blog lê
func saida(sv []aion2.Server) {
	type linha = []any // [nome, classe, cp, titulo, legiao, servidorId]
	idx := map[string]any{"atualizado": time.Now().UTC().Format(time.RFC3339)}
	cen := map[string]any{}
	geral := []linha{}
	for _, s := range sv {
		var S Servidor
		if !ler(arq(s), &S) {
			continue
		}
		var L []*Pers
		cl := map[int]int{}
		faixa := map[string]int{}
		cpc := map[int][]int{}
		com := 0
		for _, p := range S.P {
			cl[p.C]++
			switch {
			case p.L >= 45:
				faixa["45"]++
			case p.L >= 30:
				faixa["30-44"]++
			case p.L >= 10:
				faixa["10-29"]++
			default:
				faixa["1-9"]++
			}
			if p.L >= 45 && p.CP > 0 && p.Q != "" && dias(p.Q) <= 14 {
				L = append(L, p)
				cpc[p.C] = append(cpc[p.C], p.CP)
				com++
			}
		}
		sort.Slice(L, func(i, j int) bool { return L[i].CP > L[j].CP })
		top := []linha{}
		porc := map[int]int{}
		for i, p := range L {
			if len(top) < 100 || porc[p.C] < 50 {
				top = append(top, linha{p.N, p.C, p.CP, p.T, p.G})
				porc[p.C]++
			}
			if i < 300 {
				geral = append(geral, linha{p.N, p.C, p.CP, p.T, p.G, s.ServerID})
			}
		}
		med := map[int][]int{}
		for k, v := range cpc {
			sort.Ints(v)
			q := func(f float64) int { return v[int(f*float64(len(v)-1))] }
			med[k] = []int{len(v), q(0.5), q(0.9), q(0.99), v[len(v)-1]}
		}
		gravar(fmt.Sprintf("site/ranking/%d.json", s.ServerID), map[string]any{"s": s.ServerID, "nome": s.Name, "raca": s.RaceID, "top": top, "com_cp": com}, false)
		cen[fmt.Sprint(s.ServerID)] = map[string]any{"nome": s.Name, "raca": s.RaceID, "total": len(S.P), "classes": cl, "niveis": faixa, "cp_classe": med, "lista": S.Lista, "varrendo": S.Prox > 0}
	}
	sort.Slice(geral, func(i, j int) bool { return geral[i][2].(int) > geral[j][2].(int) })
	if len(geral) > 300 {
		geral = geral[:300]
	}
	gravar("site/ranking/geral.json", geral, false)
	censoCompleto(sv)
	gravar("site/censo.json", cen, false)
	gravar("site/indice.json", idx, true)
}

// patch: lê as últimas notas de atualização e avisos oficiais (em português) e anota as novas
func patch() {
	type nota struct {
		ID     string `json:"id"`
		Tipo   string `json:"tipo"`
		Titulo string `json:"titulo"`
		Resumo string `json:"resumo"`
		Data   string `json:"data"`
		Link   string `json:"link"`
	}
	vistos := map[string]bool{}
	primeira := !ler("estado/patch-vistos.json", &vistos)
	if vistos == nil {
		vistos = map[string]bool{}
	}
	var lista, novos []nota
	quadros := []struct {
		b    aion2.Board
		nome string
	}{{aion2.BoardPatchNotes, "Atualização"}, {aion2.BoardNotices, "Aviso"}, {aion2.BoardDevNews, "Notícia"}}
	for _, q := range quadros {
		n := 0
		for p, err := range c.Posts(ctx, q.b) {
			if err != nil {
				fmt.Println("erro", q.nome, err)
				break
			}
			r := []rune(strings.TrimSpace(p.Summary))
			if len(r) > 280 {
				r = append(r[:277], []rune("...")...)
			}
			x := nota{p.ID, q.nome, strings.TrimSpace(p.Title), string(r), p.PostedAt.Format("2006-01-02"), "https://aion2.plaync.com/pt-br/board/" + string(q.b) + "/view?articleId=" + p.ID}
			lista = append(lista, x)
			if !vistos[p.ID] {
				vistos[p.ID] = true
				if !primeira {
					novos = append(novos, x)
				}
			}
			n++
			if n >= 15 {
				break
			}
		}
	}
	if len(lista) == 0 {
		fmt.Println("nenhuma nota lida")
		os.Exit(1)
	}
	sort.SliceStable(lista, func(i, j int) bool { return lista[i].Data > lista[j].Data })
	gravar("site/patch.json", map[string]any{"atualizado": time.Now().UTC().Format(time.RFC3339), "notas": lista}, false)
	gravar("estado/patch-vistos.json", vistos, false)
	gravar("novos-avisos.json", novos, false)
	fmt.Println("notas", len(lista), "novas", len(novos), "primeira", primeira)
}

// censoCompleto: os mesmos blocos da página Censo LatAm, montados com a lista oficial
func censoCompleto(sv []aion2.Server) {
	nomeCl := map[int]string{2: "Gladiador", 3: "Templário", 4: "Arqueiro", 5: "Assassino", 6: "Espiritualista", 7: "Feiticeiro", 8: "Clérigo", 9: "Encantador"}
	fac := func(r int) string {
		if r == 1 {
			return "E"
		}
		return "A"
	}
	hist := map[string][]int{} // "classe|E/A/T" -> CP de cada nível 45 consultado
	cls := map[string]map[string]int{}
	niv := map[string]map[string]int{"E": {}, "A": {}}
	type leg struct {
		srv, nome string
		n, cp     int
	}
	legs := map[string]*leg{}
	tit := map[string][]int{}
	ativos := map[string]int{"E": 0, "A": 0}
	var srv [][]any
	for _, s := range sv {
		var S Servidor
		if !ler(arq(s), &S) || len(S.P) == 0 {
			continue
		}
		f := fac(s.RaceID)
		porCl := map[int]int{}
		n45 := 0
		legS := map[string]int{}
		for _, p := range S.P {
			nc := nomeCl[p.C]
			if cls[nc] == nil {
				cls[nc] = map[string]int{}
			}
			cls[nc][f]++
			porCl[p.C]++
			switch {
			case p.L >= 45:
				niv[f]["45"]++
				n45++
			case p.L >= 30:
				niv[f]["30-44"]++
			case p.L >= 10:
				niv[f]["10-29"]++
			default:
				niv[f]["1-9"]++
			}
			if p.L < 45 || p.CP <= 0 || p.Q == "" || dias(p.Q) > 30 {
				continue
			}
			if p.M != "" && dias(p.M) <= 30 {
				ativos[f]++
			}
			for _, k := range []string{nc + "|" + f, nc + "|T", "|" + f, "|T"} {
				hist[k] = append(hist[k], p.CP)
			}
			if p.G != "" {
				k := fmt.Sprint(s.ServerID, "|", p.G)
				if legs[k] == nil {
					legs[k] = &leg{srv: s.Name, nome: p.G}
				}
				legs[k].n++
				legs[k].cp += p.CP
				legS[p.G] += p.CP
			}
			if p.T != "" {
				if tit[p.T] == nil {
					tit[p.T] = []int{0, 0}
				}
				if f == "E" {
					tit[p.T][0]++
				} else {
					tit[p.T][1]++
				}
			}
		}
		top, topN := 0, -1
		for k, v := range porCl {
			if v > topN {
				top, topN = k, v
			}
		}
		lg, lcp := "", 0
		for k, v := range legS {
			if v > lcp {
				lg, lcp = k, v
			}
		}
		srv = append(srv, []any{s.Name, s.ServerID, s.RaceID, len(S.P), len(legS), nomeCl[top], lg, lcp, n45})
	}
	// P: [quantos, depois por mil com CP >= 0, 10 mil, 20 mil ... 300 mil]
	P := map[string][]int{}
	for k, v := range hist {
		a := []int{len(v)}
		for i := 0; i <= 30; i++ {
			c := 0
			for _, x := range v {
				if x >= i*10000 {
					c++
				}
			}
			a = append(a, (c*1000+len(v)/2)/len(v))
		}
		P[k] = a
	}
	var L []*leg
	for _, l := range legs {
		L = append(L, l)
	}
	sort.Slice(L, func(i, j int) bool { return L[i].cp > L[j].cp })
	var legOut [][]any
	for i, l := range L {
		if i >= 10 {
			break
		}
		legOut = append(legOut, []any{l.srv, l.nome, l.n, l.cp})
	}
	type tt struct {
		t string
		v []int
	}
	var T []tt
	for k, v := range tit {
		T = append(T, tt{k, v})
	}
	sort.Slice(T, func(i, j int) bool { return T[i].v[0]+T[i].v[1] > T[j].v[0]+T[j].v[1] })
	var titOut [][]any
	for i, t := range T {
		if i >= 10 {
			break
		}
		titOut = append(titOut, []any{t.t, t.v})
	}
	gravar("site/censo-completo.json", map[string]any{
		"atualizado": time.Now().UTC().Format(time.RFC3339),
		"srv":        srv,    // [servidor, id, raça, personagens, legiões, classe mais jogada, legião mais forte, CP dela, nível 45]
		"cls":        cls,    // classe -> {E, A} (todos os níveis)
		"niveis":     niv,    // facção -> faixa de nível -> quantos
		"P":          P,      // "classe|E/A/T" -> [nível 45 com CP, por mil com CP >= 0, 10 mil ... 300 mil]
		"legs":       legOut, // [servidor, legião, membros nível 45, soma do CP]
		"titles":     titOut, // [título, [Elyos, Asmodianos]]
		"ativos":     ativos, // nível 45 cujo CP mudou nos últimos 30 dias
	}, false)
}
