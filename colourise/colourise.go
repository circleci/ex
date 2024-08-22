package colourise

import (
	"fmt"
	"hash/crc32"
)

// colours is all ansi colour codes that look ok against black
var colours = []uint8{
	9, 10, 11, 12, 13, 14, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43,
	44, 45, 46, 47, 48, 49, 50, 51, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72, 73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83,
	84, 85, 86, 87, 92, 93, 94, 95, 96, 97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112,
	113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132, 133, 134, 135,
	136, 137, 138, 139, 140, 141, 142, 143, 144, 146, 147, 148, 149, 150, 151, 152, 153, 154, 155, 156, 157, 158,
	59, 160, 161, 162, 163, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173, 174, 175, 176, 177, 178, 179, 180, 181,
	182, 183, 184, 185, 186, 187, 188, 189, 190, 191, 192, 193, 194, 195, 196, 197, 198, 199, 200, 201, 202, 203, 204,
	205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 216, 217, 218, 219, 220, 221, 222, 223, 224, 225, 226, 227,
	228, 229, 230, 231,
}

var colourCount = uint32(len(colours)) //nolint:gosec

// ApplyColour returns the passed string adorned with the ANSI escape sequences
// to color the text in a random colour that looks good against a black
// background. Colors are picked with a deterministic hash of the string,
// meaning colours are consistent for the same string across runs.  The string
// returned terminates with the ANSI escape sequence to reset the colourization.
func ApplyColour(value string) string {
	i := crc32.Checksum([]byte(value), crc32.IEEETable) % colourCount
	return fmt.Sprintf("\033[1;38;5;%dm%s\033[0m", colours[i], value)
}

// ErrorHighlight returns the passed string adorned with ANSI escape codes to
// render the text as a highlighted error.
func ErrorHighlight(s string) string {
	return fmt.Sprintf("\033[1;37;41m%s\033[0m", s)
}
