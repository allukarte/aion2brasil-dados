# AION 2 Brasil · dados

Dados públicos usados pelo blog [aion2brasil.com.br](https://www.aion2brasil.com.br), atualizados por um robô (GitHub Actions) todo dia.

- `site/ranking/<servidor>.json`: ranking de CP por servidor (top 100 + 50 por classe), só nível 45.
- `site/ranking/geral.json`: top 300 de CP do servidor SA (LatAm).
- `site/censo.json`: personagens por classe e faixa de nível, e CP por classe (mediana, 90%, 99%, máximo).
- `site/censo-completo.json`: os blocos da página Censo LatAm (servidores, facções, classes, faixas de CP, legiões e títulos mais usados).
- `site/patch.json`: últimas notas de atualização e avisos oficiais em português, lidas a cada 2 horas. Cada nota nova abre uma issue neste repositório, que serve de alerta.

Fonte: site oficial do AION 2 (aion2.plaync.com), consultado com a biblioteca [nuriland/aion2-api](https://github.com/nuriland/aion2-api) (MIT). Site de fã, sem vínculo com a NCSOFT.
