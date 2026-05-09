package toolcall

import (
	"strings"
	"unicode/utf8"
)

func normalizeDSMLToolCallMarkup(text string) (string, bool) {
	if text == "" {
		return "", true
	}
	hasAliasLikeMarkup, _ := ContainsToolMarkupSyntaxOutsideIgnored(text)
	if !hasAliasLikeMarkup {
		return text, true
	}
	return rewriteDSMLToolMarkupOutsideIgnored(text), true
}

func rewriteDSMLToolMarkupOutsideIgnored(text string) string {
	if text == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); {
		next, advanced, blocked := skipXMLIgnoredSection(text, i)
		if blocked {
			b.WriteString(text[i:])
			break
		}
		if advanced {
			b.WriteString(text[i:next])
			i = next
			continue
		}
		tag, ok := scanToolMarkupTagAt(text, i)
		if !ok {
			b.WriteByte(text[i])
			i++
			continue
		}
		if tag.DSMLLike {
			b.WriteByte('<')
			if tag.Closing {
				b.WriteByte('/')
			}
			b.WriteString(tag.Name)
			tail := normalizeToolMarkupTagTailForXML(text[tag.NameEnd : tag.End+1])
			b.WriteString(tail)
			if !strings.HasSuffix(tail, ">") {
				b.WriteByte('>')
			}
			i = tag.End + 1
			continue
		}
		b.WriteString(text[tag.Start : tag.End+1])
		i = tag.End + 1
	}
	return b.String()
}

func normalizeToolMarkupTagTailForXML(tail string) string {
	if tail == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(tail))
	for i := 0; i < len(tail); {
		r, size := utf8.DecodeRuneInString(tail[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteByte(tail[i])
			i++
			continue
		}
		switch normalizeFullwidthASCII(r) {
		case '>', '/', '=', '"', '\'':
			b.WriteRune(normalizeFullwidthASCII(r))
		default:
			b.WriteString(tail[i : i+size])
		}
		i += size
	}
	return b.String()
}
