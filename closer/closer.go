/*
Package closer contains a helper function for not losing deferred errors
*/
package closer

import "io"

func ErrorHandler(c io.Closer, in *error) {
	cerr := c.Close()
	if *in == nil {
		*in = cerr
	}
}
