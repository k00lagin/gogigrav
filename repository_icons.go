package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/fnv"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"unicode"

	"github.com/gogpu/gg/svg"
	_ "github.com/sergeymakinen/go-ico"
	"gogigrav/internal/repo"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/unicode/norm"
)

const repositoryIconSize = 20

var iconExtensions = []string{"svg", "ico", "icns", "png", "jpg", "jpeg", "gif", "webp", "bmp"}
var knownRepositoryIcons = strings.Fields(`favicon.svg favicon.ico favicon.png
	public/favicon.svg public/favicon.ico public/favicon.png app/favicon.ico app/favicon.png
	app/icon.svg app/icon.png app/icon.ico src/favicon.ico src/favicon.svg src/app/favicon.ico
	src/app/icon.svg src/app/icon.png assets/icon.svg assets/icon.png assets/logo.svg assets/logo.png
	.idea/icon.svg Resources/AppIcon.icns AppIcon.icns`)
var iconSources = strings.Fields("index.html public/index.html app/routes/__root.tsx src/routes/__root.tsx app/root.tsx src/root.tsx src/index.html")
var iconLink = regexp.MustCompile(`(?i)<link\b[^<>]*>`)
var iconRel = regexp.MustCompile(`(?i)\brel\s*[=:]\s*["'](?:shortcut\s+)?icon["']`)
var iconHref = regexp.MustCompile(`(?i)\bhref\s*[=:]\s*["']([^"']+)["']`)

func repositoryKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

func repositoryMonogram(name string) string {
	words := strings.FieldsFunc(norm.NFKC.String(name), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return "PR"
	}
	first := []rune(words[0])
	letters := []rune{unicode.ToUpper(first[0])}
	if len(words) > 1 {
		letters = append(letters, unicode.ToUpper([]rune(words[1])[0]))
	} else {
		for i := 1; i < len(first); i++ {
			if unicode.IsDigit(first[i]) || (unicode.IsLower(first[i-1]) && unicode.IsUpper(first[i])) {
				letters = append(letters, first[i])
				break
			}
		}
	}
	return string(letters)
}

func repositoryColor(name string) uint32 {
	colors := [...]uint32{0xB65B43, 0x986C15, 0x66813D, 0x337D73, 0x487BB4, 0x6658B7, 0xA05282}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return colors[hash.Sum32()%uint32(len(colors))]
}

type iconThumbnail struct {
	path string
	img  image.Image
}

// readProjectFile follows links only within the opened repository.
func readProjectFile(root *os.Root, name string, limit int64, prefix bool) ([]byte, error) {
	f, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readIconBytes(f, limit, prefix)
}

func readIconBytes(f *os.File, limit int64, prefix bool) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || (!prefix && info.Size() > limit) {
		return nil, errors.New("image is not a regular file within the size limit")
	}
	if prefix {
		return io.ReadAll(io.LimitReader(f, limit))
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("image exceeds the size limit")
	}
	return data, err
}

func iconSupported(path string) bool {
	return slices.Contains(iconExtensions, strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."))
}

func loadThumbnail(root *os.Root, path string, external bool) (iconThumbnail, error) {
	if !iconSupported(path) {
		return iconThumbnail{}, errors.New("unsupported image format")
	}
	const maxBytes = 16 << 20
	var data []byte
	var err error
	if external && filepath.IsAbs(path) {
		f, e := os.Open(path)
		if e != nil {
			return iconThumbnail{}, e
		}
		defer f.Close()
		data, err = readIconBytes(f, maxBytes, false)
	} else {
		data, err = readProjectFile(root, path, maxBytes, false)
	}
	if err != nil {
		return iconThumbnail{}, err
	}
	img, err := decodeThumbnail(data, strings.ToLower(filepath.Ext(path)))
	return iconThumbnail{path: path, img: img}, err
}

func iconDimensions(width, height float64) (int, int) {
	scale := repositoryIconSize / math.Max(width, height)
	return max(1, int(math.Round(width*scale))), max(1, int(math.Round(height*scale)))
}

func decodeThumbnail(data []byte, ext string) (image.Image, error) {
	if ext == ".svg" {
		doc, err := svg.Parse(data)
		if err != nil {
			return nil, err
		}
		if !(doc.ViewBox.Width > 0 && doc.ViewBox.Height > 0) || len(doc.Elements) == 0 {
			return nil, errors.New("empty SVG")
		}
		w, h := iconDimensions(doc.ViewBox.Width, doc.ViewBox.Height)
		return doc.Render(w, h), nil
	}
	if ext == ".icns" {
		// Modern ICNS files contain PNG renditions at several resolutions.
		if len(data) < 8 || string(data[:4]) != "icns" {
			return nil, errors.New("invalid ICNS file")
		}
		var best []byte
		for chunks := data[8:]; len(chunks) >= 8; {
			n := int64(binary.BigEndian.Uint32(chunks[4:8]))
			if n < 8 || n > int64(len(chunks)) {
				break
			}
			part := chunks[8:n]
			if bytes.HasPrefix(part, []byte("\x89PNG\r\n\x1a\n")) && (best == nil || len(part) < len(best)) {
				best = part
			}
			chunks = chunks[n:]
		}
		if best == nil {
			return nil, errors.New("ICNS contains no PNG rendition")
		}
		data = best
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 16<<20 {
		return nil, errors.New("image dimensions exceed the preview limit")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := iconDimensions(float64(cfg.Width), float64(cfg.Height))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst, nil
}

func resolveRepositoryIcon(path, override string) iconThumbnail {
	root, err := os.OpenRoot(path)
	if err != nil {
		return iconThumbnail{}
	}
	defer root.Close()
	if override != "" {
		if thumb, err := loadThumbnail(root, override, true); err == nil {
			return thumb
		}
	}
	var project struct{ IconPath string }
	if raw, err := readProjectFile(root, "t3.json", 128<<10, false); err == nil && json.Unmarshal(raw, &project) == nil && project.IconPath != "" {
		if thumb, err := loadThumbnail(root, project.IconPath, false); err == nil {
			return thumb
		}
	}
	for _, path := range knownRepositoryIcons {
		if thumb, err := loadThumbnail(root, path, false); err == nil {
			return thumb
		}
	}
	for _, source := range iconSources {
		raw, err := readProjectFile(root, source, 128<<10, true)
		if err != nil {
			continue
		}
		text := string(raw)
		links := iconLink.FindAllString(text, -1)
		if strings.HasSuffix(source, ".tsx") {
			links = append(links, strings.Split(text, "}")...)
		}
		for _, link := range links {
			if !iconRel.MatchString(link) {
				continue
			}
			match := iconHref.FindStringSubmatch(link)
			if len(match) == 0 {
				continue
			}
			u, err := url.Parse(match[1])
			if err != nil || u.IsAbs() || u.Host != "" {
				continue
			}
			path := strings.TrimPrefix(u.Path, "/")
			if !filepath.IsLocal(filepath.FromSlash(path)) {
				continue
			}
			for _, candidate := range []string{"public/" + path, path} {
				if thumb, err := loadThumbnail(root, candidate, false); err == nil {
					return thumb
				}
			}
		}
	}
	return iconThumbnail{}
}

func projectImageFiles(ctx context.Context, root string) ([]string, error) {
	raw, err := repo.Run(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var files []string
	for path := range strings.SplitSeq(raw, "\x00") {
		if iconSupported(path) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if info, err := dir.Stat(filepath.FromSlash(path)); err == nil && info.Mode().IsRegular() {
				files = append(files, path)
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}
