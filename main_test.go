package main

import (
	"net/url"
	"testing"

	"github.com/kr/pretty"
)

var urls = []string{
	"file:/var/lib/goback",
	"s3:/var/lib/goback",
	"/var/lib/goback",
	"http://backup/user/namespace",
	"memory:",
	"./test",
	"test",
}

func TestUrlParse(t *testing.T) {
	for _, testUrl := range urls {
		u, err := url.Parse(testUrl)
		if err != nil {
			t.Error(err)
		}

		pretty.Print(u)
	}
}
