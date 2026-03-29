package chunk

import (
	"regexp"
	"strings"
)

var multiNewlineRE = regexp.MustCompile(`\n{3,}`)

func CleanText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = multiNewlineRE.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

func Split(text string, chunkSize int, overlap int) []string {
	if chunkSize <= 0 || overlap < 0 || overlap >= chunkSize {
		return nil
	}

	text = CleanText(text)
	if text == "" {
		return nil
	}

	paragraphs := strings.Split(text, "\n\n")
	chunks := make([]string, 0)
	current := ""

	for _, p := range paragraphs {
		para := strings.TrimSpace(p)
		if para == "" {
			continue
		}

		candidate := para
		if current != "" {
			candidate = strings.TrimSpace(current + "\n\n" + para)
		}

		if len(candidate) <= chunkSize {
			current = candidate
			continue
		}

		if current != "" {
			chunks = append(chunks, current)
			tail := ""
			if overlap > 0 && len(current) > overlap {
				tail = current[len(current)-overlap:]
			} else if overlap > 0 {
				tail = current
			}
			current = strings.TrimSpace(tail + "\n\n" + para)
		} else {
			current = para
		}

		for len(current) > chunkSize {
			chunks = append(chunks, strings.TrimSpace(current[:chunkSize]))
			step := chunkSize - overlap
			if step < 1 {
				step = 1
			}
			current = strings.TrimSpace(current[step:])
		}
	}

	if current != "" {
		chunks = append(chunks, current)
	}

	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}
