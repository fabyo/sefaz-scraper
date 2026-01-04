package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func unzipAndExtractXSD(data []byte, dest string) error {
	r := bytes.NewReader(data)
	zipReader, err := zip.NewReader(r, int64(len(data)))
	if err != nil {
		return fmt.Errorf("erro ao abrir o leitor de zip: %v", err)
	}
	if err := os.MkdirAll(dest, os.ModePerm); err != nil {
		return fmt.Errorf("erro ao criar diretório de destino: %v", err)
	}
	for _, f := range zipReader.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".xsd") {
			continue
		}
		fileName := filepath.Base(f.Name)
		fpath := filepath.Join(dest, fileName)
		log.Printf("📦 Extraindo XSD: %s", fpath)
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("erro ao criar arquivo de destino %s: %v", fpath, err)
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return fmt.Errorf("erro ao abrir arquivo %s de dentro do zip: %v", f.Name, err)
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return fmt.Errorf("erro ao copiar conteúdo para %s: %v", fpath, err)
		}
	}
	return nil
}

type ReleasePackage struct {
	URL  string
	Date time.Time
	Text string
}

var dateRegex = regexp.MustCompile(`(\d{2}/\d{2}/\d{2,4})`)

func parseDate(dateStr string) (time.Time, error) {
	if len(dateStr) == 8 {
		dateStr = dateStr[:6] + "20" + dateStr[6:]
	}
	layout := "02/01/2006"
	t, err := time.Parse(layout, dateStr)
	if err != nil {
		layout = "02/01/06"
		t, err = time.Parse(layout, dateStr)
	}
	return t, err
}

func getRenderedHTML(pageURL string) (string, []*http.Cookie, error) {
	log.Println("--- 🤖 Iniciando ChromeDP (navegador real) ---")

	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var htmlContent string
	var cookies []*network.Cookie
	var err error
	
	err = chromedp.Run(ctx,
		chromedp.Navigate(pageURL),
		network.SetExtraHTTPHeaders(map[string]interface{}{
			"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Accept-Language": "pt-BR,pt;q=0.9",
		}),
		chromedp.WaitVisible(`#conteudo`, chromedp.ByID),
		chromedp.ActionFunc(func(ctx context.Context) error {
			log.Println("--- ✅ Página carregada e JS executado. Lendo HTML... ---")
			cookies, err = network.GetCookies().Do(ctx)
			if err != nil {
				return err
			}
			return nil
		}),
		chromedp.OuterHTML("html", &htmlContent),
	)

	if err != nil {
		return "", nil, fmt.Errorf("erro no ChromeDP: %v", err)
	}

	log.Println("--- ✅ HTML final e Cookies capturados com sucesso ---")

	httpCookies := make([]*http.Cookie, 0, len(cookies))
	for _, c := range cookies {
		httpCookies = append(httpCookies, &http.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Domain: c.Domain,
			Path:   c.Path,
		})
	}

	return htmlContent, httpCookies, nil
}

func parseHTML(htmlContent, baseURL string) ([]ReleasePackage, error) {
	var packages []ReleasePackage
	currentSection := ""

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	doc.Find("#conteudoDinamico .tituloSessao, #conteudoDinamico .indentacaoNormal p").Each(func(i int, s *goquery.Selection) {
		if s.Is(".tituloSessao") {
			textLower := strings.ToLower(s.Text())
			if strings.Contains(textLower, "versões oficiais") {
				log.Println("--- 🟢 Entrando na seção 'Versões Oficiais' ---")
				currentSection = "OFICIAIS"
			} else if strings.Contains(textLower, "versões anteriores") {
				log.Println("--- 🟡 Entrando na seção 'Versões Anteriores' ---")
				currentSection = "ANTERIORES"
			} else if strings.Contains(textLower, "versões para testes") {
				log.Println("--- 🔴 Entrando na seção 'Testes' (Ignorando) ---")
				currentSection = "TESTES"
			}
			return
		}

		if s.Is("p") {
			if currentSection != "OFICIAIS" && currentSection != "ANTERIORES" {
				return
			}
			aTag := s.Find("a")
			link, exists := aTag.Attr("href")
			if !exists {
				return
			}
			fullParagraphText := s.Text()
			fullParagraphTextLower := strings.ToLower(fullParagraphText)
			if !strings.Contains(fullParagraphTextLower, "(zip)") {
				if !strings.Contains(aTag.Text(), "ZIP") {
					return
				}
			}
			if !(strings.Contains(fullParagraphTextLower, "pacote de liberação") || strings.Contains(fullParagraphTextLower, "esquema xml")) {
				return
			}
			match := dateRegex.FindStringSubmatch(fullParagraphText)
			if len(match) < 2 {
				log.Printf("⚠️ Link ignorado (sem data): %s", aTag.Text())
				return
			}
			dateStr := match[1]
			pubDate, err := parseDate(dateStr)
			if err != nil {
				log.Printf("⚠️ Erro ao parsear data '%s' para: %s", dateStr, aTag.Text())
				return
			}
			
			// 🚀 MUDANÇA: Remove filtro de ano - pega TODOS os pacotes de 2017 em diante
			if pubDate.Year() < 2017 {
				return // Ainda ignora muito antigos (pré-v4.00)
			}
			
			cleanedLink := strings.TrimSpace(link)
			cleanedLink = strings.Join(strings.Fields(cleanedLink), "")
			
			parsedLink, err := url.Parse(cleanedLink)
			if err != nil {
				log.Printf("⚠️ Erro ao parsear link '%s': %v", cleanedLink, err)
				return
			}

			absoluteURL := parsedBaseURL.ResolveReference(parsedLink).String()
			pkg := ReleasePackage{
				URL:  absoluteURL,
				Date: pubDate,
				Text: aTag.Text(),
			}
			packages = append(packages, pkg)
			log.Printf("📝 Encontrado: %s (Data: %s)", pkg.Text, pubDate.Format("02/01/2006"))
		}
	})
	return packages, nil
}

func main() {
	const extractionDir = "schemas/v4"
	const sefazURL = "https://www.nfe.fazenda.gov.br/portal/listaConteudo.aspx?tipoConteudo=BMPFMBoln3w="

	log.Println("🚀 Iniciando o scraper... (Modo: ChromeDP)")

	htmlContent, cookies, err := getRenderedHTML(sefazURL)
	if err != nil {
		log.Fatalf("🚫 Falha ao carregar a página: %v", err)
	}

	packagesToDownload, err := parseHTML(htmlContent, sefazURL)
	if err != nil {
		log.Fatalf("🚫 Falha ao parsear o HTML: %v", err)
	}

	log.Println("-----------------------------------------------------")
	log.Println("---  leitura da página finalizada ---")
	if len(packagesToDownload) == 0 {
		log.Println("Nenhum pacote encontrado. Encerrando.")
		return
	}

	log.Printf("Total de %d pacotes relevantes encontrados. Ordenando por data...", len(packagesToDownload))
	sort.Slice(packagesToDownload, func(i, j int) bool {
		return packagesToDownload[i].Date.Before(packagesToDownload[j].Date)
	})
	log.Println("Pacotes ordenados. Iniciando downloads e extração em ordem...")
	log.Println("-----------------------------------------------------")

	jar, _ := cookiejar.New(nil)
	cookieURL, _ := url.Parse(sefazURL)
	jar.SetCookies(cookieURL, cookies) 

	client := &http.Client{
		Jar: jar,
	}

	for _, pkg := range packagesToDownload {
		log.Printf("🚀 Processando (Data: %s): %s", pkg.Date.Format("2006-01-02"), pkg.Text)

		req, _ := http.NewRequest("GET", pkg.URL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		req.Header.Set("Referer", sefazURL)

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("❌ Erro ao BAIXAR %s: %v", pkg.URL, err)
			continue
		}

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "zip") && !strings.Contains(contentType, "octet-stream") {
			resp.Body.Close()
			continue
		}
		
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Printf("❌ Erro ao LER o body de %s: %v", pkg.URL, err)
			continue
		}

		if err := unzipAndExtractXSD(body, extractionDir); err != nil {
			log.Printf("❌ Erro ao DESCOMPACTAR %s: %v", pkg.URL, err)
		} else {
			log.Printf("✅ Sucesso ao processar %s", pkg.URL)
		}
	}

	log.Println("--- ✅ Processamento de todos os pacotes concluído! ---")
	log.Println("🏁 Script finalizado.")
}