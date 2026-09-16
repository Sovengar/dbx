package editor

import (
	"strings"

	"github.com/buble/dbx/internal/theme"
)

var sqlKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "AND": true, "OR": true,
	"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true, "SET": true,
	"DELETE": true, "CREATE": true, "TABLE": true, "ALTER": true, "DROP": true,
	"INDEX": true, "VIEW": true, "TRIGGER": true, "FUNCTION": true, "PROCEDURE": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true,
	"ON": true, "AS": true, "ORDER": true, "BY": true, "GROUP": true,
	"HAVING": true, "LIMIT": true, "OFFSET": true, "DISTINCT": true,
	"UNION": true, "ALL": true, "EXCEPT": true, "INTERSECT": true,
	"IN": true, "NOT": true, "NULL": true, "IS": true, "LIKE": true,
	"BETWEEN": true, "EXISTS": true, "ANY": true, "SOME": true,
	"CASE": true, "WHEN": true, "THEN": true, "ELSE": true, "END": true,
	"ASC": true, "DESC": true, "TRUE": true, "FALSE": true,
	"COUNT": true, "SUM": true, "AVG": true, "MIN": true, "MAX": true,
	"COALESCE": true, "CAST": true, "CONVERT": true,
	"RETURNING": true, "WITH": true, "RECURSIVE": true,
	"GRANT": true, "REVOKE": true, "COMMIT": true, "ROLLBACK": true,
	"BEGIN": true, "TRANSACTION": true, "SAVEPOINT": true,
	"PRIMARY": true, "KEY": true, "FOREIGN": true, "REFERENCES": true,
	"UNIQUE": true, "CHECK": true, "DEFAULT": true, "CONSTRAINT": true,
	"AUTO_INCREMENT": true, "SERIAL": true, "BIGSERIAL": true,
	"BOOLEAN": true, "INTEGER": true, "BIGINT": true, "SMALLINT": true,
	"TEXT": true, "VARCHAR": true, "CHAR": true, "UUID": true,
	"TIMESTAMP": true, "DATE": true, "TIME": true, "INTERVAL": true,
	"JSON": true, "JSONB": true, "ARRAY": true, "ENUM": true,
	"NOW": true, "CURRENT_TIMESTAMP": true,
	"IF": true, "REPLACE": true, "TRUNCATE": true,
	"ANALYZE": true, "VACUUM": true, "EXPLAIN": true,
}

var sqlFunctions = map[string]bool{
	"NOW": true, "CURRENT_TIMESTAMP": true, "CURRENT_DATE": true,
	"EXTRACT": true, "DATE_TRUNC": true, "TO_CHAR": true, "TO_DATE": true,
	"STRING_AGG": true, "ARRAY_AGG": true, "JSON_AGG": true,
	"ROW_NUMBER": true, "RANK": true, "DENSE_RANK": true,
	"LEAD": true, "LAG": true, "FIRST_VALUE": true, "LAST_VALUE": true,
	"CONCAT": true, "CONCAT_WS": true, "LENGTH": true, "TRIM": true,
	"UPPER": true, "LOWER": true, "INITCAP": true, "SUBSTRING": true,
	"REPLACE": true, "REVERSE": true, "POSITION": true, "STRPOS": true,
	"ABS": true, "ROUND": true, "CEIL": true, "FLOOR": true, "MOD": true,
	"POWER": true, "SQRT": true, "LN": true, "LOG": true,
	"EXISTS": true, "NOT": true,
}

type sqlTokenType int

const (
	tokenDefault sqlTokenType = iota
	tokenKeyword
	tokenString
	tokenNumber
	tokenComment
	tokenFunction
	tokenOperator
)

type sqlToken struct {
	typ   sqlTokenType
	text  string
	start int
	end   int
}

// tokenizeSQL scans the input by byte so token offsets line up with the
// editor's cursor column. Bytes >= 0x80 are treated as part of a word so UTF-8
// runes are never split.
func tokenizeSQL(input string) []sqlToken {
	var tokens []sqlToken
	i := 0
	n := len(input)

	for i < n {
		ch := input[i]

		// Single-line comment
		if ch == '-' && i+1 < n && input[i+1] == '-' {
			start := i
			for i < n && input[i] != '\n' {
				i++
			}
			tokens = append(tokens, sqlToken{tokenComment, input[start:i], start, i})
			continue
		}

		// Block comment
		if ch == '/' && i+1 < n && input[i+1] == '*' {
			start := i
			i += 2
			for i < n-1 {
				if input[i] == '*' && input[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			tokens = append(tokens, sqlToken{tokenComment, input[start:i], start, i})
			continue
		}

		// String literal
		if ch == '\'' {
			start := i
			i++
			for i < n {
				if input[i] == '\'' {
					if i+1 < n && input[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			tokens = append(tokens, sqlToken{tokenString, input[start:i], start, i})
			continue
		}

		// Number
		if isASCIIDigit(ch) {
			start := i
			for i < n && (isASCIIDigit(input[i]) || input[i] == '.') {
				i++
			}
			tokens = append(tokens, sqlToken{tokenNumber, input[start:i], start, i})
			continue
		}

		// Word (keyword or identifier)
		if isASCIILetter(ch) || ch == '_' || ch >= 0x80 {
			start := i
			for i < n && (isASCIILetter(input[i]) || isASCIIDigit(input[i]) || input[i] == '_' || input[i] >= 0x80) {
				i++
			}
			word := input[start:i]
			upper := strings.ToUpper(word)

			token := sqlToken{tokenDefault, word, start, i}
			if sqlKeywords[upper] {
				token.typ = tokenKeyword
			} else if sqlFunctions[upper] {
				token.typ = tokenFunction
			}
			tokens = append(tokens, token)
			continue
		}

		// Operators
		if isOperatorByte(ch) {
			start := i
			i++
			// Handle <=, >=, <>, !=
			if i < n && (input[i] == '=' || input[i] == '>') {
				next := input[i]
				if (ch == '<' && next == '>') || (ch == '!' && next == '=') || (ch == '<' && next == '=') || (ch == '>' && next == '=') {
					i++
				}
			}
			tokens = append(tokens, sqlToken{tokenOperator, input[start:i], start, i})
			continue
		}

		// Parentheses, commas, semicolons, dots
		if ch == '(' || ch == ')' || ch == ',' || ch == ';' || ch == '.' {
			tokens = append(tokens, sqlToken{tokenDefault, input[i : i+1], i, i + 1})
			i++
			continue
		}

		// Whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			start := i
			for i < n && (input[i] == ' ' || input[i] == '\t' || input[i] == '\n' || input[i] == '\r') {
				i++
			}
			tokens = append(tokens, sqlToken{tokenDefault, input[start:i], start, i})
			continue
		}

		// Unknown character
		tokens = append(tokens, sqlToken{tokenDefault, input[i : i+1], i, i + 1})
		i++
	}

	return tokens
}

func isASCIILetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isOperatorByte(ch byte) bool {
	switch ch {
	case '=', '<', '>', '!', '+', '-', '*', '/', '%':
		return true
	}
	return false
}

func HighlightSQL(input string, styles *theme.Styles) string {
	if input == "" {
		return ""
	}

	tokens := tokenizeSQL(input)
	var result strings.Builder

	for _, tok := range tokens {
		switch tok.typ {
		case tokenKeyword:
			result.WriteString(styles.Primary.Render(strings.ToUpper(tok.text)))
		case tokenString:
			result.WriteString(styles.Success.Render(tok.text))
		case tokenNumber:
			result.WriteString(styles.Warning.Render(tok.text))
		case tokenComment:
			result.WriteString(styles.TextMuted.Render(tok.text))
		case tokenFunction:
			result.WriteString(styles.Info.Render(tok.text))
		case tokenOperator:
			result.WriteString(styles.TextBright.Render(tok.text))
		default:
			result.WriteString(tok.text)
		}
	}

	return result.String()
}
