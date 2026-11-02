// Package repair rebuilds an exported EPUB from its own chapter documents:
// continuation pages are merged into their chapter, navigation is regenerated
// from the surviving chapters and site advertisement lines are dropped.
package repair

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Eurekaimer/yuri_scraper/internal/converter"
	"github.com/Eurekaimer/yuri_scraper/internal/textclean"
	"github.com/PuerkitoBio/goquery"
)

// paragraphGap is the indentation that separated two paragraphs in a collapsed
// Yamibo post: the site renders the 2em first-line indent as a wide space run.
var paragraphGap = regexp.MustCompile(`[ \t]{4,}`)

// Stats reports what one repair pass did.
type Stats struct {
	Chapters     int `json:"chapters"`
	MergedPages  int `json:"merged_pages"`
	Paragraphs   int `json:"paragraphs"`
	RemovedLines int `json:"removed_lines"`
}

// Build reads the EPUB at inputPath and writes the repaired book to output.
// The input file is never modified.
func Build(inputPath string, output io.Writer) (Stats, error) {
	var stats Stats
	archive, err := zip.OpenReader(inputPath)
	if err != nil {
		return stats, fmt.Errorf("%s: open epub: %w", inputPath, err)
	}
	defer archive.Close()
	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		entries[file.Name] = file
	}

	opfPath, err := packagePath(entries)
	if err != nil {
		return stats, fmt.Errorf("%s: %w", inputPath, err)
	}
	opfBytes, err := readEntry(entries, opfPath)
	if err != nil {
		return stats, fmt.Errorf("%s: %w", inputPath, err)
	}
	var opf opfPackage
	if err := xml.Unmarshal(opfBytes, &opf); err != nil {
		return stats, fmt.Errorf("%s: parse %s: %w", inputPath, opfPath, err)
	}
	chapters := chapterEntries(entries, opfPath, opf)
	if len(chapters) == 0 {
		return stats, fmt.Errorf("%s: no chapter documents found in %s", inputPath, opfPath)
	}

	source := converter.ChapterSource(func(yield func(converter.Chapter) error) error {
		for index, chapterPath := range chapters {
			raw, err := readEntry(entries, chapterPath)
			if err != nil {
				return fmt.Errorf("%s: %w", inputPath, err)
			}
			document, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
			if err != nil {
				return fmt.Errorf("%s: parse %s: %w", inputPath, chapterPath, err)
			}
			title := strings.TrimSpace(document.Find("head title").First().Text())
			if title == "" {
				title = strings.TrimSpace(document.Find("h1").First().Text())
			}
			if title == "" {
				title = path.Base(chapterPath)
			}
			title = textclean.Paragraph(title)
			if converter.IsPageTitle(title) {
				stats.MergedPages++
			}
			// Older Yamibo exports collapsed <br>-separated paragraphs into one
			// <p>. The source indentation survives as a wide run of spaces.
			var parts []string
			document.Find("body p").Each(func(_ int, paragraph *goquery.Selection) {
				line := strings.Trim(paragraph.Text(), " \t\r\n\u3000")
				if line == "" {
					return
				}
				if textclean.IsAdLine(line) {
					stats.RemovedLines++
					return
				}
				parts = append(parts, paragraphGap.Split(line, -1)...)
			})
			var body strings.Builder
			for _, part := range parts {
				cleaned := textclean.Paragraph(part)
				if cleaned == "" {
					continue
				}
				stats.Paragraphs++
				if body.Len() > 0 {
					body.WriteByte('\n')
				}
				body.WriteString(cleaned)
			}
			if err := yield(converter.Chapter{Index: index + 1, Title: title, Content: body.String()}); err != nil {
				return err
			}
		}
		return nil
	})

	// Chapters counts what survives merging, not the pages that were read.
	merged := converter.MergePageContinuations(source)
	counted := converter.ChapterSource(func(yield func(converter.Chapter) error) error {
		return merged(func(chapter converter.Chapter) error {
			stats.Chapters++
			return yield(chapter)
		})
	})

	if err := converter.WriteEPUBStream(output, opf.bookMeta(), counted); err != nil {
		return Stats{}, fmt.Errorf("%s: write epub: %w", inputPath, err)
	}
	return stats, nil
}

func readEntry(entries map[string]*zip.File, name string) ([]byte, error) {
	file, ok := entries[name]
	if !ok {
		return nil, fmt.Errorf("missing archive entry %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open archive entry %s: %w", name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read archive entry %s: %w", name, err)
	}
	return content, nil
}

func packagePath(entries map[string]*zip.File) (string, error) {
	raw, err := readEntry(entries, "META-INF/container.xml")
	if err != nil {
		return "", err
	}
	var container struct {
		Rootfiles []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(raw, &container); err != nil {
		return "", fmt.Errorf("parse META-INF/container.xml: %w", err)
	}
	for _, rootfile := range container.Rootfiles {
		if rootfile.FullPath != "" {
			return rootfile.FullPath, nil
		}
	}
	return "", fmt.Errorf("META-INF/container.xml has no rootfile full-path")
}

type opfPackage struct {
	Metadata struct {
		Titles       []string `xml:"title"`
		Creators     []string `xml:"creator"`
		Languages    []string `xml:"language"`
		Descriptions []string `xml:"description"`
		Subjects     []string `xml:"subject"`
		Metas        []struct {
			Name     string `xml:"name,attr"`
			Content  string `xml:"content,attr"`
			Property string `xml:"property,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []manifestItem `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		ItemRefs []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}

type manifestItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

func (p opfPackage) bookMeta() converter.BookMeta {
	meta := converter.BookMeta{
		Title:       firstNonEmpty(p.Metadata.Titles...),
		Author:      strings.Join(nonEmpty(p.Metadata.Creators...), ", "),
		Language:    firstNonEmpty(p.Metadata.Languages...),
		Description: firstNonEmpty(p.Metadata.Descriptions...),
		Tags:        nonEmpty(p.Metadata.Subjects...),
	}
	if meta.Language == "" {
		meta.Language = "zh-CN"
	}
	for _, item := range p.Metadata.Metas {
		if item.Name == "protagonist" {
			meta.Protagonist = item.Content
			break
		}
	}
	return meta
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}

// chapterEntries returns the spine documents that hold chapter text: XHTML
// items that are not the navigation document. Books exported before the spine
// was populated fall back to their chapter_*.xhtml entries in numeric order.
func chapterEntries(entries map[string]*zip.File, opfPath string, opf opfPackage) []string {
	manifest := make(map[string]manifestItem, len(opf.Manifest.Items))
	for _, item := range opf.Manifest.Items {
		manifest[item.ID] = item
	}
	chapters := make([]string, 0, len(opf.Spine.ItemRefs))
	for _, ref := range opf.Spine.ItemRefs {
		item, ok := manifest[ref.IDRef]
		if !ok || item.MediaType != "application/xhtml+xml" || hasProperty(item.Properties, "nav") {
			continue
		}
		href := item.Href
		if unescaped, err := url.PathUnescape(href); err == nil {
			href = unescaped
		}
		name := path.Join(path.Dir(opfPath), href)
		if _, ok := entries[name]; ok {
			chapters = append(chapters, name)
		}
	}
	if len(chapters) > 0 {
		return chapters
	}
	type numbered struct {
		name  string
		index int
	}
	var fallback []numbered
	for name := range entries {
		base := path.Base(name)
		if !strings.HasPrefix(base, "chapter_") || !strings.HasSuffix(base, ".xhtml") {
			continue
		}
		digits := strings.TrimSuffix(strings.TrimPrefix(base, "chapter_"), ".xhtml")
		index, err := strconv.Atoi(digits)
		if err != nil {
			continue
		}
		fallback = append(fallback, numbered{name: name, index: index})
	}
	sort.Slice(fallback, func(i, j int) bool {
		if fallback[i].index != fallback[j].index {
			return fallback[i].index < fallback[j].index
		}
		return fallback[i].name < fallback[j].name
	})
	names := make([]string, 0, len(fallback))
	for _, item := range fallback {
		names = append(names, item.name)
	}
	return names
}

func hasProperty(properties, target string) bool {
	for _, property := range strings.Fields(properties) {
		if property == target {
			return true
		}
	}
	return false
}
