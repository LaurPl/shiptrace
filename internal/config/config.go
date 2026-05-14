// Package config will load and validate ~/.shiptrace/config.yaml starting
// on day 3 when the git ship adapter needs to read ship_paths. The file
// exists today so the import graph is stable and future packages can refer
// to it without churn.
package config
