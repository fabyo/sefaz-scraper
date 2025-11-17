# 🧾 sefaz-scraper

Scraper em Go que acessa diretamente o portal da SEFAZ, baixa todos os arquivos **XSD** disponíveis e organiza tudo em uma estrutura de pastas bonitinha no repositório.  
Além disso, o projeto já está preparado para rodar em **GitHub Actions** e manter esses XSD sempre atualizados automaticamente.

Perfeito para projetos que trabalham com **NF-e / CT-e / MDF-e / eventos SEFAZ** e querem ter os XSD localmente, versionados e sempre frescos.

---

## 🚀 Visão geral

- Faz scraping do site da SEFAZ usando **Chromedp** (controle de Chrome/Chromium via código).
- Usa **goquery** para parsear o HTML e localizar os links dos XSD.
- Faz download dos XSD e salva tudo em uma pasta configurável (ex: `./schemas`).
- Pode ser rodado localmente **ou** via **GitHub Actions** em um agendamento (cron) para manter o repo sempre atualizado.
- Ideal para ser usado como **submódulo** ou **módulo auxiliar** em outros projetos fiscais.

---

## 🧠 Stack

- **Linguagem:** Go `1.24.0`  
- **Toolchain:** `go1.24.10`

### Dependências principais

```go
require (
    github.com/PuerkitoBio/goquery v1.11.0
    github.com/chromedp/cdproto    v0.0.0-20250724212937-08a3db8b4327
    github.com/chromedp/chromedp   v0.14.2
)
