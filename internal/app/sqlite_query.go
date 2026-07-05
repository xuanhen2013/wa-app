package app

import "strings"

func sqliteInClause(expression string, values []string) (string, []any) {
	values = uniqueNonEmptyStrings(values...)
	if len(values) == 0 {
		return "1=0", nil
	}
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	return expression + " IN (" + strings.Join(placeholders, ", ") + ")", args
}
