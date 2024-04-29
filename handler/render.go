package handler

import (
	"bytes"
	"github.com/PuerkitoBio/goquery"
	"github.com/russross/blackfriday/v2"
	"html/template"
	"os"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/models"
)

// RenderViewHandler renders base.html template with another template that defines page specific {{"content"}}.
// Also grabs the current auth context if available, so auth template data is available on all views.
func RenderViewHandler(view string, data interface{}) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Load specified view
		tmpl, err := template.ParseFiles("./public/views/base.html", view)
		if err != nil {
			return err
		}

		// Load components
		tmpl, err = tmpl.ParseGlob("./public/components/*.tmpl")
		if err != nil {
			return err
		}

		// Define common and custom data
		record, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		tmplData := map[string]interface{}{}
		if record != nil {
			tmplData["Avatar"] = record.Get("avatar")
			tmplData["Paid"] = record.Get("paid")
			tmplData["Verified"] = record.Get("verified")
			tmplData["Record"] = record
		}

		// Render View
		return tmpl.Execute(c.Response().Writer, tmplData)
	}
}

// RenderDocViewHandler takes a markdown file, renders it into HTML, and then renders it a template with a right navigation sidebar.
func RenderDocViewHandler(dir string) echo.HandlerFunc {
	return func(c echo.Context) error {

		doc := c.PathParam("doc")

		// Load markdown
		markdownContent, err := os.ReadFile(dir + doc + ".md")
		if err != nil {
			return err
		}

		// Parse markdown into goquery document
		htmlDocContent := blackfriday.Run(markdownContent)
		queryDoc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlDocContent))
		if err != nil {
			return err
		}

		// Load headings to add right sidebar navigation with slug id
		type Heading struct {
			Text string
			ID   string
		}
		var headings []Heading

		// Tailwind themes
		queryDoc.Find("h1, h2, h3, h4, h5, h6").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			slug := strings.ToLower(text)
			slug = strings.ReplaceAll(slug, " ", "-")
			headings = append(headings, Heading{Text: text, ID: slug})
			s.SetAttr("id", slug)
		})

		// Headings
		queryDoc.Find("h1").Each(func(i int, s *goquery.Selection) {
			s.AddClass("text-3xl font-extrabold tracking-tight text-gray-900 dark:text-white p-2")
		})
		queryDoc.Find("h2, h3, h4, h5, h6").Each(func(i int, s *goquery.Selection) {
			s.AddClass("ext-xl font-extrabold tracking-tight text-gray-900 dark:text-white p-2")
		})

		queryDoc.Find("p").Each(func(i int, s *goquery.Selection) {
			s.AddClass("text-gray-500 dark:text-gray-400 p-2")
		})

		queryDoc.Find("pre").Each(func(i int, s *goquery.Selection) {
			s.AddClass("rounded-lg border dark:border-gray-700 my-4")
		})
		queryDoc.Find("code").Each(func(i int, s *goquery.Selection) {
			s.AddClass("rounded-lg")
		})

		queryDoc.Find("a").Each(func(i int, s *goquery.Selection) {
			s.AddClass("font-medium text-blue-600 dark:text-blue-500 hover:underline")
		})

		queryDoc.Find("ul").Each(func(i int, s *goquery.Selection) {
			s.AddClass("max-w-md space-y-1 text-gray-500 list-disc list-inside dark:text-gray-400 p-2")
		})
		queryDoc.Find("ol").Each(func(i int, s *goquery.Selection) {
			s.AddClass("max-w-md space-y-1 text-gray-500 list-decimal list-inside dark:text-gray-400")
		})

		// Render document into HTML after goquery processing
		renderedHtmlDoc, err := queryDoc.Html()
		if err != nil {
			return err
		}

		// Load view
		tmpl, err := template.ParseFiles("./public/views/base.html", "./public/views/docs.html")
		if err != nil {
			return err
		}

		// Load components
		tmpl, err = tmpl.ParseGlob("./public/components/*.tmpl")
		if err != nil {
			return err
		}

		// Define common and custom data
		record, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		// Setup injected template data
		tmplData := map[string]interface{}{}
		tmplData["Content"] = template.HTML(renderedHtmlDoc)
		tmplData["Headings"] = headings
		tmplData["Doc"] = strings.Title(strings.ReplaceAll(doc, "-", " "))
		if record != nil {
			tmplData["Avatar"] = record.Get("avatar")
			tmplData["Paid"] = record.Get("paid")
			tmplData["Verified"] = record.Get("verified")
			tmplData["Record"] = record
		}

		// Render View
		return tmpl.Execute(c.Response().Writer, tmplData)

	}
}
