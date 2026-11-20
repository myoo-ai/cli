package textpos

// LineBounds returns the byte bounds of 1-based line, without its LF or CRLF
// terminator. starts holds the byte offset where each line starts, and line
// has to be in range.
func LineBounds[S ~string | ~[]byte](src S, starts []int, line int) (start, end int) {
	start = starts[line-1]
	end = len(src)
	if line < len(starts) {
		end = starts[line] - 1
	}
	if end > start && src[end-1] == '\r' {
		end--
	}
	return start, end
}
