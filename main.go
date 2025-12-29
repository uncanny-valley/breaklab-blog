package main

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var templateFuncs = template.FuncMap{
	"formatSlug": func(s string) string {
		s = strings.ReplaceAll(s, "-", " ")
		s = strings.ReplaceAll(s, "_", " ")
		return strings.Title(s)
	},
	"hashColor": func(s string) int {
		var hash uint32
		for _, c := range s {
			hash = hash*31 + uint32(c)
		}
		return int(hash % 5)
	},
}

func parseTemplates(files ...string) (*template.Template, error) {
	return template.New(filepath.Base(files[0])).Funcs(templateFuncs).ParseFiles(files...)
}

type Post struct {
	Slug                  string
	Title                 string
	Description           template.HTML
	Date                  string
	RawDate               string
	Collection            string
	CollectionTitle       string
	CollectionDescription template.HTML
	CollectionIndex       int
	CollectionTotal       int
	Content               template.HTML
	ReadTimeInMinutes     int
	TOC                   []TOCItem
	PageType              string
}

type TOCItem struct {
	ID    string
	Text  string
	Level int
}

type Collection struct {
	Slug            string
	Title           string
	Description     template.HTML
	DescriptionText string
	Posts           []Post
	PageType        string
}

type IndexData struct {
	Title    string
	Posts    []Post
	PageType string
}

type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel *Channel `xml:"channel"`
}

type Channel struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Items       []Item `xml:"item"`
}

type Item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	GUID        string `xml:"guid"`
}

type CollectionsData struct {
	Title       string
	Collections []Collection
	PageType    string
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "build" {
		baseURL := "https://example.com"
		if len(os.Args) > 2 {
			baseURL = os.Args[2]
		}
		if err := buildStatic(baseURL); err != nil {
			log.Fatal(err)
		}
		return
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/post/", handlePost)
	http.HandleFunc("/collections", handleCollections)
	http.HandleFunc("/collection/", handleCollection)
	http.HandleFunc("/feed.xml", handleRSS)
	http.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "robots.txt")
	})
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server starting on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func buildStatic(baseURL string) error {
	distDir := "dist"

	// Clean and create dist directory
	os.RemoveAll(distDir)
	os.MkdirAll(distDir, 0755)

	// Load posts and collections
	posts, err := loadPosts()
	if err != nil {
		return err
	}
	collections, err := loadCollections()
	if err != nil {
		return err
	}

	// Build index page
	fmt.Println("Building index.html...")
	if err := buildPage(distDir+"/index.html", "templates/layout.html", "templates/index.html",
		IndexData{Title: "", Posts: posts, PageType: "index"}); err != nil {
		return err
	}

	// Build post pages
	for _, post := range posts {
		post.PageType = "post"
		dir := distDir + "/post/" + post.Slug
		os.MkdirAll(dir, 0755)
		fmt.Printf("Building post/%s/index.html...\n", post.Slug)
		if err := buildPage(dir+"/index.html", "templates/layout.html", "templates/post.html", post); err != nil {
			return err
		}
	}

	// Build collections index page
	fmt.Println("Building collections/index.html...")
	os.MkdirAll(distDir+"/collections", 0755)
	if err := buildPage(distDir+"/collections/index.html", "templates/layout.html", "templates/collections.html",
		CollectionsData{Title: "Collections", Collections: collections, PageType: "collections"}); err != nil {
		return err
	}

	// Build individual collection pages
	for _, collection := range collections {
		collection.PageType = "collection"
		dir := distDir + "/collection/" + collection.Slug
		os.MkdirAll(dir, 0755)
		fmt.Printf("Building collection/%s/index.html...\n", collection.Slug)
		if err := buildPage(dir+"/index.html", "templates/layout.html", "templates/collection.html", collection); err != nil {
			return err
		}
	}

	// Build RSS feed
	fmt.Println("Building feed.xml...")
	if err := buildRSSFeed(distDir+"/feed.xml", baseURL, posts); err != nil {
		return err
	}

	// Copy static assets
	fmt.Println("Copying static assets...")
	if err := copyDir("static", distDir+"/static"); err != nil {
		return err
	}

	// Copy robots.txt
	if _, err := os.Stat("robots.txt"); err == nil {
		fmt.Println("Copying robots.txt...")
		copyFile("robots.txt", distDir+"/robots.txt")
	}

	fmt.Println("Build complete! Output in ./dist")
	return nil
}

func buildPage(outputPath, layoutPath, contentPath string, data interface{}) error {
	tmpl, err := parseTemplates(layoutPath, contentPath)
	if err != nil {
		return err
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.ExecuteTemplate(f, "layout", data)
}

func buildRSSFeed(outputPath, baseURL string, posts []Post) error {
	var items []Item
	for _, post := range posts {
		pubDate := ""
		if t, err := time.Parse("2006-01-02", post.RawDate); err == nil {
			pubDate = t.Format(time.RFC1123Z)
		}
		description := string(post.Description)
		if description == "" {
			description = string(post.Content)
		}
		items = append(items, Item{
			Title:       post.Title,
			Link:        fmt.Sprintf("%s/post/%s", baseURL, post.Slug),
			Description: description,
			PubDate:     pubDate,
			GUID:        fmt.Sprintf("%s/post/%s", baseURL, post.Slug),
		})
	}

	feed := RSS{
		Version: "2.0",
		Channel: &Channel{
			Title:       "BreakLab",
			Link:        baseURL,
			Description: "Blog posts from BreakLab",
			Items:       items,
		},
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(xml.Header)
	encoder := xml.NewEncoder(f)
	encoder.Indent("", "  ")
	return encoder.Encode(feed)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}
		return copyFile(path, dstPath)
	})
}

func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0644)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	posts, err := loadPosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl, err := parseTemplates("templates/layout.html", "templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := IndexData{Title: "", Posts: posts, PageType: "index"}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/post/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	post, err := loadPost(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post.PageType = "post"

	tmpl, err := parseTemplates("templates/layout.html", "templates/post.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", post); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleCollections(w http.ResponseWriter, r *http.Request) {
	collections, err := loadCollections()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tmpl, err := parseTemplates("templates/layout.html", "templates/collections.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := CollectionsData{Title: "Collections", Collections: collections, PageType: "collections"}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleCollection(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/collection/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	collection, err := loadCollection(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	collection.PageType = "collection"

	tmpl, err := parseTemplates("templates/layout.html", "templates/collection.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout", collection); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleRSS(w http.ResponseWriter, r *http.Request) {
	posts, err := loadPosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the base URL from the request
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	var items []Item
	for _, post := range posts {
		// Parse date and convert to RFC822 format for RSS
		pubDate := ""
		if t, err := time.Parse("2006-01-02", post.RawDate); err == nil {
			pubDate = t.Format(time.RFC1123Z)
		}

		description := string(post.Description)
		if description == "" {
			description = string(post.Content)
		}

		items = append(items, Item{
			Title:       post.Title,
			Link:        fmt.Sprintf("%s/post/%s", baseURL, post.Slug),
			Description: description,
			PubDate:     pubDate,
			GUID:        fmt.Sprintf("%s/post/%s", baseURL, post.Slug),
		})
	}

	feed := RSS{
		Version: "2.0",
		Channel: &Channel{
			Title:       "BreakLab",
			Link:        baseURL,
			Description: "Blog posts from BreakLab",
			Items:       items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(feed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func loadCollections() ([]Collection, error) {
	var collections []Collection

	err := filepath.WalkDir("collections", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		slug := strings.TrimSuffix(filepath.Base(path), ".html")
		collection, err := loadCollection(slug)
		if err != nil {
			return err
		}
		collections = append(collections, collection)
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by title
	sort.Slice(collections, func(i, j int) bool {
		return collections[i].Title < collections[j].Title
	})

	return collections, nil
}

func loadPosts() ([]Post, error) {
	var posts []Post

	err := filepath.WalkDir("posts", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		slug := strings.TrimSuffix(filepath.Base(path), ".html")
		post, err := loadPost(slug)
		if err != nil {
			return err
		}
		posts = append(posts, post)
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(posts, func(i, j int) bool {
		return posts[i].RawDate > posts[j].RawDate
	})

	return posts, nil
}

func loadCollection(slug string) (Collection, error) {
	content, err := os.ReadFile(filepath.Join("collections", slug+".html"))
	if err != nil {
		return Collection{}, err
	}

	lines := strings.Split(string(content), "\n")
	description := strings.TrimSpace(extractContent(lines))

	collection := Collection{
		Slug:            slug,
		Title:           extractMeta(lines, "title"),
		Description:     template.HTML(description),
		DescriptionText: stripHTML(description),
	}

	// Load all posts and filter by collection
	posts, err := loadPosts()
	if err != nil {
		return Collection{}, err
	}

	for _, post := range posts {
		if post.Collection == slug {
			collection.Posts = append(collection.Posts, post)
		}
	}

	return collection, nil
}

func getCollectionPosition(currentSlug, collectionSlug, currentDate string) (int, int) {
	type postInfo struct {
		slug string
		date string
	}
	var postsInCollection []postInfo

	filepath.WalkDir("posts", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		if extractMeta(lines, "collection") == collectionSlug {
			slug := strings.TrimSuffix(filepath.Base(path), ".html")
			date := extractMeta(lines, "date")
			postsInCollection = append(postsInCollection, postInfo{slug: slug, date: date})
		}
		return nil
	})

	// Sort by date ascending (oldest first)
	sort.Slice(postsInCollection, func(i, j int) bool {
		return postsInCollection[i].date < postsInCollection[j].date
	})

	total := len(postsInCollection)
	index := 0
	for i, p := range postsInCollection {
		if p.slug == currentSlug {
			index = i + 1 // 1-based index
			break
		}
	}

	return index, total
}

func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	text := re.ReplaceAllString(s, "")
	// Collapse whitespace
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func loadPost(slug string) (Post, error) {
	content, err := os.ReadFile(filepath.Join("posts", slug+".html"))
	if err != nil {
		return Post{}, err
	}

	lines := strings.Split(string(content), "\n")
	rawContent := extractContent(lines)

	// Process content to add IDs to headings and extract TOC
	processedContent, toc := processContentWithTOC(rawContent)

	rawDate := extractMeta(lines, "date")
	if rawDate == "" {
		rawDate = time.Now().Format("2006-01-02")
	}
	formattedDate := rawDate
	if t, err := time.Parse("2006-01-02", rawDate); err == nil {
		formattedDate = t.Format("January 2, 2006")
	}

	collectionSlug := extractMeta(lines, "collection")
	var collectionTitle string
	var collectionDescription template.HTML
	var collectionIndex, collectionTotal int
	if collectionSlug != "" {
		if collectionContent, err := os.ReadFile(filepath.Join("collections", collectionSlug+".html")); err == nil {
			collectionLines := strings.Split(string(collectionContent), "\n")
			collectionTitle = extractMeta(collectionLines, "title")
			collectionDescription = template.HTML(strings.TrimSpace(extractContent(collectionLines)))
		}
		// Calculate position in collection
		collectionIndex, collectionTotal = getCollectionPosition(slug, collectionSlug, rawDate)
	}

	post := Post{
		Slug:                  slug,
		Title:                 extractMeta(lines, "title"),
		Description:           template.HTML(extractMeta(lines, "description")),
		Date:                  formattedDate,
		RawDate:               rawDate,
		Collection:            collectionSlug,
		CollectionTitle:       collectionTitle,
		CollectionDescription: collectionDescription,
		CollectionIndex:       collectionIndex,
		CollectionTotal:       collectionTotal,
		Content:               template.HTML(processedContent),
		TOC:                   toc,
	}
	if post.Title == "" {
		post.Title = slug
	}

	words := len(strings.Fields(string(post.Content)))

	// compute the reading time in minutes assuming 200 words / min
	// cap at 1 minute reading time
	post.ReadTimeInMinutes = int(math.Max(float64(words)/200, 1.0))

	return post, nil
}

func extractMeta(lines []string, key string) string {
	prefix := "<!-- " + key + ": "
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, prefix), " -->"))
		}
	}
	return ""
}

func extractContent(lines []string) string {
	var contentLines []string
	metaKeys := []string{"title:", "date:", "description:", "collection:"}
	for _, line := range lines {
		if strings.HasPrefix(line, "<!--") {
			isMeta := false
			for _, key := range metaKeys {
				if strings.Contains(line, key) {
					isMeta = true
					break
				}
			}
			if isMeta {
				continue
			}
		}
		contentLines = append(contentLines, line)
	}
	return strings.Join(contentLines, "\n")
}

func generateID(text string) string {
	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	text = re.ReplaceAllString(text, "")

	// Convert to lowercase and replace spaces with hyphens
	text = strings.ToLower(text)
	text = strings.TrimSpace(text)

	// Replace non-alphanumeric characters with hyphens
	re = regexp.MustCompile(`[^a-z0-9]+`)
	text = re.ReplaceAllString(text, "-")

	// Remove leading/trailing hyphens
	text = strings.Trim(text, "-")

	return text
}

func processContentWithTOC(content string) (string, []TOCItem) {
	var toc []TOCItem

	// Match h2 and h3 tags
	h2Regex := regexp.MustCompile(`<h2>(.*?)</h2>`)
	h3Regex := regexp.MustCompile(`<h3>(.*?)</h3>`)

	// Process h2 tags
	content = h2Regex.ReplaceAllStringFunc(content, func(match string) string {
		text := h2Regex.FindStringSubmatch(match)[1]
		id := generateID(text)
		toc = append(toc, TOCItem{ID: id, Text: text, Level: 2})
		return fmt.Sprintf(`<h2 id="%s">%s</h2>`, id, text)
	})

	// Process h3 tags
	content = h3Regex.ReplaceAllStringFunc(content, func(match string) string {
		text := h3Regex.FindStringSubmatch(match)[1]
		id := generateID(text)
		toc = append(toc, TOCItem{ID: id, Text: text, Level: 3})
		return fmt.Sprintf(`<h3 id="%s">%s</h3>`, id, text)
	})

	return content, toc
}
