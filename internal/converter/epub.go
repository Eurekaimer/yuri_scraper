package converter

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"
)

// WriteEPUB writes an EPUB 3 file from chapter data.
// EPUB is essentially a ZIP with specific XML files.
func WriteEPUB(w io.Writer, meta BookMeta) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	bookID := fmt.Sprintf("yuri_scraper-%s", now)

	// Normalize language, default zh-CN
	lang := meta.Language
	if lang == "" {
		lang = "zh-CN"
	}

	// Apply simplified conversion to content
	title := orDefault(meta.Title, "Untitled")
	if IsTraditional(title) {
		title = ToSimplified(title)
	}
	author := orDefault(meta.Author, "Unknown")
	if IsTraditional(author) {
		author = ToSimplified(author)
	}

	// Merge and auto-detect tags
	tags := meta.Tags
	if len(tags) == 0 {
		tags = AutoDetectTags(meta.Title, meta.Description)
	} else {
		tags = MergeTags(meta.Tags, AutoDetectTags(meta.Title, meta.Description))
	}

	// mimetype (must be first, uncompressed)
	mimeHeader := &zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	}
	mw, err := zw.CreateHeader(mimeHeader)
	if err != nil {
		return err
	}
	mw.Write([]byte("application/epub+zip"))

	// META-INF/container.xml
	containerXML := `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`
	writeZipFile(zw, "META-INF/container.xml", containerXML)

	// Build chapter XHTML files
	var chapterFiles []string
	for i, ch := range meta.Chapters {
		fname := fmt.Sprintf("chapter_%03d.xhtml", i+1)
		chapterFiles = append(chapterFiles, fname)

		// Convert chapter content to simplified
		content := ch.Content
		if IsTraditional(content) {
			content = ToSimplified(content)
		}
		chapterTitle := ch.Title
		if IsTraditional(chapterTitle) {
			chapterTitle = ToSimplified(chapterTitle)
		}

		escapedContent := htmlEscape(content)
		paragraphs := strings.Split(escapedContent, "\n")
		var htmlContent strings.Builder
		htmlContent.WriteString(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh">
<head>
<title>%s</title>
<link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
<h1>%s</h1>
`, htmlEscape(chapterTitle), htmlEscape(chapterTitle)))

		for _, p := range paragraphs {
			p = strings.TrimSpace(p)
			if p != "" {
				htmlContent.WriteString(fmt.Sprintf("<p>%s</p>\n", p))
			}
		}
		htmlContent.WriteString("</body>\n</html>")

		writeZipFile(zw, "OEBPS/"+fname, htmlContent.String())
	}

	// CSS for reading comfort
	css := `body {
  font-family: "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "WenQuanYi Micro Hei", "Noto Sans SC", sans-serif;
  line-height: 1.8;
  margin: 1.5em 1em;
  color: #2c2c2c;
}
h1 { font-size: 1.4em; margin: 1em 0 0.5em; text-align: center; }
p { text-indent: 2em; margin: 0.5em 0; }`
	writeZipFile(zw, "OEBPS/style.css", css)

	// Add style.css to manifest
	chapterFiles = append(chapterFiles, "style.css")

	// content.opf with full metadata
	opf := buildOPF(bookID, title, author, lang, meta.Description, meta.Protagonist, tags, chapterFiles)
	writeZipFile(zw, "OEBPS/content.opf", opf)

	// toc.ncx (for backwards compatibility)
	ncx := buildNCX(bookID, title, meta.Chapters)
	writeZipFile(zw, "OEBPS/toc.ncx", ncx)

	// nav.xhtml (EPUB 3 navigation)
	nav := buildNav(meta.Chapters)
	writeZipFile(zw, "OEBPS/nav.xhtml", nav)

	return nil
}

func writeZipFile(zw *zip.Writer, name, content string) {
	w, err := zw.Create(name)
	if err != nil {
		panic(fmt.Sprintf("zip create %s: %v", name, err))
	}
	io.WriteString(w, content)
}

func buildOPF(id, title, author, lang, description, protagonist string, tags, chapters []string) string {
	var manifest strings.Builder
	var spine strings.Builder
	var metaBuilder strings.Builder

	// toc.ncx
	manifest.WriteString(`    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>` + "\n")
	manifest.WriteString(`    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` + "\n")

	spine.WriteString(`    <itemref idref="nav" linear="no"/>` + "\n")

	for _, fname := range chapters {
		if fname == "style.css" {
			manifest.WriteString(`    <item id="css" href="style.css" media-type="text/css"/>` + "\n")
			continue
		}
		cid := strings.TrimSuffix(fname, ".xhtml")
		manifest.WriteString(fmt.Sprintf(`    <item id="%s" href="%s" media-type="application/xhtml+xml"/>`+"\n", cid, fname))
		spine.WriteString(fmt.Sprintf(`    <itemref idref="%s"/>`+"\n", cid))
	}

	// Build metadata block with enhanced fields
	metaBuilder.WriteString(fmt.Sprintf(`    <dc:identifier id="book-id">%s</dc:identifier>`+"\n", htmlEscape(id)))
	metaBuilder.WriteString(fmt.Sprintf(`    <dc:title>%s</dc:title>`+"\n", htmlEscape(title)))
	metaBuilder.WriteString(fmt.Sprintf(`    <dc:creator>%s</dc:creator>`+"\n", htmlEscape(author)))
	metaBuilder.WriteString(fmt.Sprintf(`    <dc:language>%s</dc:language>`+"\n", lang))
	if description != "" {
		metaBuilder.WriteString(fmt.Sprintf(`    <dc:description>%s</dc:description>`+"\n", htmlEscape(description)))
	}
	if protagonist != "" {
		metaBuilder.WriteString(fmt.Sprintf(`    <meta name="protagonist" content="%s"/>`+"\n", htmlEscape(protagonist)))
	}
	for _, tag := range tags {
		metaBuilder.WriteString(fmt.Sprintf(`    <dc:subject>%s</dc:subject>`+"\n", htmlEscape(tag)))
	}
	metaBuilder.WriteString(fmt.Sprintf(`    <meta property="dcterms:modified">%s</meta>`+"\n", time.Now().UTC().Format(time.RFC3339)))

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="book-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
%s
  </metadata>
  <manifest>
%s
  </manifest>
  <spine>
%s
  </spine>
</package>`, metaBuilder.String(), manifest.String(), spine.String())
}

func buildNCX(id, title string, chapters []Chapter) string {
	var navMap strings.Builder
	playOrder := 1
	for _, ch := range chapters {
		fname := fmt.Sprintf("chapter_%03d.xhtml", ch.Index)
		navMap.WriteString(fmt.Sprintf(
			`    <navPoint id="navpoint-%d" playOrder="%d">
      <navLabel><text>%s</text></navLabel>
      <content src="%s"/>
    </navPoint>
`, ch.Index, playOrder, htmlEscape(ch.Title), fname))
		playOrder++
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE ncx PUBLIC "-//NISO//DTD ncx 2005-1//EN" "http://www.daisy.org/z3986/2005/ncx-2005-1.dtd">
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head>
    <meta name="dtb:uid" content="%s"/>
    <meta name="dtb:depth" content="1"/>
    <meta name="dtb:totalPageCount" content="0"/>
    <meta name="dtb:maxPageNumber" content="0"/>
  </head>
  <docTitle><text>%s</text></docTitle>
  <navMap>
%s
  </navMap>
</ncx>`, id, htmlEscape(title), navMap.String())
}

func buildNav(chapters []Chapter) string {
	var ol strings.Builder
	for _, ch := range chapters {
		fname := fmt.Sprintf("chapter_%03d.xhtml", ch.Index)
		ol.WriteString(fmt.Sprintf(`      <li><a href="%s">%s</a></li>`+"\n", fname, htmlEscape(ch.Title)))
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh">
<head><title>Table of Contents</title></head>
<body>
  <nav epub:type="toc">
    <h1>目录</h1>
    <ol>
%s
    </ol>
  </nav>
</body>
</html>`, ol.String())
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

var _ = bytes.NewReader // unused import guard
