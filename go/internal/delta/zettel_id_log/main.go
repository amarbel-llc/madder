package zettel_id_log

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	charlie_zil "github.com/amarbel-llc/madder/go/internal/charlie/zettel_id_log"
)

type (
	Side = charlie_zil.Side
	V1   = charlie_zil.V1
)

const (
	SideYin  = charlie_zil.SideYin
	SideYang = charlie_zil.SideYang
)

type Entry interface {
	GetSide() Side
	GetTai() ids.Tai
	GetMarklId() markl.Id
	GetWordCount() int
}

var _ Entry = V1{}
