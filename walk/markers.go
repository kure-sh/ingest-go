package walk

import (
	"go/ast"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Comment struct {
	Text    string
	Markers []string
}

func ReadComment(doc string) (comment Comment) {
	lines := strings.Split(doc, "\n")
	start := 0
	end := len(lines)

	for _, line := range lines {
		if len(line) > 0 {
			if strings.HasPrefix(line, "+") {
				comment.Markers = append(comment.Markers, strings.TrimSpace(line[1:]))
			} else {
				break
			}
		}

		start++
	}

	for i := len(lines) - 1; i >= start; i-- {
		line := lines[i]
		if len(line) > 0 {
			if strings.HasPrefix(line, "+") {
				comment.Markers = append(comment.Markers, strings.TrimSpace(line[1:]))
			} else {
				break
			}
		}

		end--
	}

	comment.Text = strings.Join(lines[start:end], "\n")
	return
}

func (c *Comment) AddMarkers(comments *ast.CommentGroup) {
	if comments == nil {
		return
	}

	var markers []string

	for _, line := range strings.Split(comments.Text(), "\n") {
		if strings.HasPrefix(line, "+") {
			markers = append(markers, strings.TrimSpace(line[1:]))
		}
	}

	if markers != nil {
		c.Markers = append(markers, c.Markers...)
	}
}

func (c *Comment) Marker(name string) string {
	prefix := name + "="

	for _, m := range c.Markers {
		if m == name {
			return "true"
		} else if strings.HasPrefix(m, prefix) {
			return strings.TrimPrefix(m, prefix)
		}
	}

	return ""
}

func (c *Comment) Deprecated() bool {
	return deprecation.MatchString(c.Text)
}

var deprecation = regexp.MustCompile(`\bDeprecated|DEPRECATED\b`)

type packageComments map[string]map[int]*ast.CommentGroup

func scanPackageComments(pkg *packages.Package) packageComments {
	comments := make(packageComments)

	for _, file := range pkg.Syntax {
		for _, comment := range file.Comments {
			pos := pkg.Fset.Position(comment.End())

			fileComments := comments[pos.Filename]
			if fileComments == nil {
				fileComments = make(map[int]*ast.CommentGroup)
				comments[pos.Filename] = fileComments
			}

			fileComments[pos.Line] = comment
		}
	}

	return comments
}

func (c packageComments) get(filename string, lastLine int) *ast.CommentGroup {
	if fc := c[filename]; fc != nil {
		return fc[lastLine]
	}

	return nil
}

// Scan a ;-separated list of quoted strings and bare strings.
func scanEnumValidation(spec string) (values []string, err error) {
	i := 0

	for i < len(spec) {
		n := strings.IndexAny(spec[i:], `";`)
		if n < 0 {
			break
		}

		switch spec[i+n] {
		case ';':
			values = append(values, spec[i:i+n])
			i += n + 1

		case '"':
			quoted, err := strconv.QuotedPrefix(spec[i:])
			if err != nil {
				return nil, err
			}

			value, err := strconv.Unquote(quoted)
			if err != nil {
				return nil, err
			}

			values = append(values, value)

			i += len(quoted)
			if i < len(spec) && spec[i] == ';' {
				i++
			}
		}
	}
	if i < len(spec) {
		values = append(values, spec[i:])
	}

	return
}
