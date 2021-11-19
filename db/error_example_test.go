package db_test

import (
	"errors"
	"fmt"

	"github.com/circleci/ex/db"
)

func ExamplePqError() {
	err := errors.New("im not the right error")

	// An example of how to extract the constraint that failed
	if errors.Is(err, db.ErrConstrained) && db.PqError(err).Constraint == "my_fk" {
		fmt.Println("do your constraint behaviour")
	} else {
		fmt.Println("not the error you are looking for")
	}
	// Alternatively you may want to go direct
	if db.PqError(err) != nil && db.PqError(err).Constraint == "my_fk" {
		// you may need to map the code here - which would be bad.

		// but you could check the db error mapping here ...
		if errors.Is(err, db.ErrConstrained) {
			fmt.Println("do your constraint behaviour")
		}
	}

	// Or with As and the PqError method
	e := &db.Error{}
	if errors.As(err, &e) && e.PqError() != nil && e.PqError().Constraint == "my_fk" {
		// do some special constraint violation handling
	}

	// Output: not the error you are looking for
}
