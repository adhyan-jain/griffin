package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNeedsHandsSQL(t *testing.T) {
	sqlEmpty := needsHandsSQL("")
	assert.Contains(t, sqlEmpty, "jsonb_array_length")

	sqlAliased := needsHandsSQL("t.")
	assert.Contains(t, sqlAliased, "t.input_payload->'requires'")
}

func TestHostCanDoSQL(t *testing.T) {
	sql := hostCanDoSQL("", "$5")
	assert.True(t, strings.Contains(sql, "$5::text[]"))
	assert.True(t, strings.Contains(sql, "input_payload->'requires'"))
}
