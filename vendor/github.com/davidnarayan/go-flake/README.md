go-flake
=====

go-flake generates unique identifiers that are roughly sortable by time. 

Flake can run on a cluster of machines and still generate unique IDs without 
requiring worker coordination.

A Flake ID is a 64-bit integer will the following components:
  - 41 bits is the timestamp with millisecond precision
  - 10 bits is the host id (uses IP modulo 2^10)
  - 13 bits is an auto-incrementing sequence for ID requests within the same millisecond

Installation
------------

```
go get github.com/davidnarayan/go-flake
```


Example
-------

```go
package main

import (
	"log"
	"github.com/davidnarayan/go-flake"
)

func main() {
	f, err := flake.New()

	if err != nil {
		log.Fatal(err)
	}

	id := f.NextId()
	fmt.Println(id)
	fmt.Println(id.String())
}
```


Credit
------

This work is based on code and concepts from the following:

  - https://blog.twitter.com/2010/announcing-snowflake
  - http://instagram-engineering.tumblr.com/post/10853187575/sharding-ids-at-instagram
