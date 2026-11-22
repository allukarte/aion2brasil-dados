# Confere a resposta de /daevanion de um personagem contra os tabuleiros da classe.
import json, sys
c = sys.argv[1]
r = json.load(open('r.json'))
d = json.load(open('site/daevanion/%s.json' % c))
chaves = {n['efeito'][0].rsplit(' +', 1)[0] for t in d['tabuleiros'] for n in t['nos'] if n['tipo'] == 'SkillLevel'}
nomes = {k[0] for k in r.get('skills', [])}
usadas = {k[0] for k in r.get('skills', []) if k[1] in ('Active', 'Dp') and k[3]}
tab = {k: (len(v) if v is not None else None) for k, v in r.get('tabuleiros', {}).items()}
print('classe', c, 'tabuleiros', tab, 'skills', len(nomes), 'nós de skill no Daevanion', len(chaves),
      'batem com o nome', len(chaves & nomes), 'usadas', len(usadas & chaves), 'sem par', sorted(chaves - nomes)[:5])
