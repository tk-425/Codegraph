package cli

import (
	"fmt"
	"io"
)

const logoBanner = `
┏━╸┏━┓╺┳┓┏━╸   ┏━╸┏━┓┏━┓┏━┓╻ ╻
┃  ┃ ┃ ┃┃┣╸ ╺━╸┃╺┓┣┳┛┣━┫┣━┛┣━┫
┗━╸┗━┛╺┻┛┗━╸   ┗━┛╹┗╸╹ ╹╹  ╹ ╹`

func printBanner(w io.Writer) {
	fmt.Fprintln(w, Bold(logoBanner))
}
