package org

type Org struct {
	Domain string
}

type Role string

const (
	Admin  Role = "admin"
	Member Role = "member"
)
