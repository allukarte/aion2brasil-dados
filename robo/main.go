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
	"time"

	aion2 "github.com/nuriland/aion2-api"
)

const pausa = 400 * time.Millisecond // educado com o site oficial (~2 pedidos por segundo)

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
	min, vm, cm := 300, 45, 60
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
	c, err = aion2.New(aion2.ConfigOpts{Region: aion2.RegionSA, Locale: aion2.LocalePTBR})
	if err != nil {
		panic(err)
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
	if limite.After(fim) {
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
	for _, a := range fila {
		if !tempo() {
			break
		}
		ch, err := c.Character(ctx, aion2.CharacterRef{ServerID: a.s, CharacterID: a.id})
		time.Sleep(pausa)
		p := all[a.s].P[a.id]
		p.Q = hoje
		if err != nil {
			continue
		}
		p.CP, p.T, p.G, p.L, p.N = ch.Profile.CombatPower, ch.Profile.TitleName, ch.Profile.GuildName, ch.Profile.Level, ch.Profile.Name
		n++
		if n%500 == 0 {
			for _, s := range sv {
				if S := all[s.ServerID]; S != nil {
					gravar(arq(s), S, false)
				}
			}
		}
	}
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
		for _, p := range L {
			if len(top) < 100 || porc[p.C] < 50 {
				top = append(top, linha{p.N, p.C, p.CP, p.T, p.G})
				porc[p.C]++
			}
			if len(geral) < 5000 {
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
	gravar("site/censo.json", cen, false)
	gravar("site/indice.json", idx, true)
}
