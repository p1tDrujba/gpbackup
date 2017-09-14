package backup

/*
 * This file contains structs and functions related to executing specific
 * queries to gather metadata for the objects handled in predata_textsearch.go.
 *
 * Text search is not supported in GPDB 4.3, so none of these structs or functions
 * are used in a 4.3 backup.
 */

import (
	"fmt"

	"github.com/greenplum-db/gpbackup/utils"
)

type TextSearchParser struct {
	Oid          uint32
	Schema       string `db:"nspname"`
	Name         string `db:"prsname"`
	StartFunc    string `db:"prsstart"`
	TokenFunc    string `db:"prstoken"`
	EndFunc      string `db:"prsend"`
	LexTypesFunc string `db:"prslextype"`
	HeadlineFunc string
}

func GetTextSearchParsers(connection *utils.DBConn) []TextSearchParser {
	query := fmt.Sprintf(`
SELECT
	p.oid,
	nspname,
	prsname,
	prsstart,
	prstoken,
	prsend,
	prslextype,
	CASE WHEN prsheadline::regproc::text = '-' THEN '' ELSE prsheadline::regproc::text END AS headlinefunc 
FROM pg_ts_parser p
JOIN pg_namespace n ON n.oid = p.prsnamespace
WHERE %s
ORDER BY prsname;`, SchemaFilterClause("n"))

	results := make([]TextSearchParser, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type TextSearchTemplate struct {
	Oid        uint32
	Schema     string
	Name       string
	InitFunc   string
	LexizeFunc string
}

func GetTextSearchTemplates(connection *utils.DBConn) []TextSearchTemplate {
	query := fmt.Sprintf(`
SELECT
	p.oid,
	nspname as schema,
	tmplname AS name,
	CASE WHEN tmplinit::regproc::text = '-' THEN '' ELSE tmplinit::regproc::text END AS initfunc,
	tmpllexize::regproc::text AS lexizefunc
FROM pg_ts_template p
JOIN pg_namespace n ON n.oid = p.tmplnamespace
WHERE %s
ORDER BY tmplname;`, SchemaFilterClause("n"))

	results := make([]TextSearchTemplate, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type TextSearchDictionary struct {
	Oid        uint32
	Schema     string
	Name       string
	Template   string
	InitOption string
}

func GetTextSearchDictionaries(connection *utils.DBConn) []TextSearchDictionary {
	query := fmt.Sprintf(`
SELECT
	d.oid,
	dict_ns.nspname as schema,
	dictname AS name,
	quote_ident(tmpl_ns.nspname) || '.' || quote_ident(t.tmplname) AS template,
	COALESCE(dictinitoption, '') AS initoption
FROM pg_ts_dict d
JOIN pg_ts_template t ON t.oid = d.dicttemplate
JOIN pg_namespace tmpl_ns ON tmpl_ns.oid = t.tmplnamespace
JOIN pg_namespace dict_ns ON dict_ns.oid = d.dictnamespace
WHERE %s
ORDER BY dictname;`, SchemaFilterClause("dict_ns"))

	results := make([]TextSearchDictionary, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)
	return results
}

type TextSearchConfiguration struct {
	Oid          uint32
	Schema       string
	Name         string
	Parser       string
	TokenToDicts map[string][]string
}

func GetTextSearchConfigurations(connection *utils.DBConn) []TextSearchConfiguration {
	query := fmt.Sprintf(`
SELECT
	c.oid AS configoid,
	cfg_ns.nspname AS schema,
	cfgname AS name,
	cfgparser AS parseroid,
	quote_ident(prs_ns.nspname) || '.' || quote_ident(prsname) AS parserfqn
FROM pg_ts_config c
JOIN pg_ts_parser p ON p.oid = c.cfgparser
JOIN pg_namespace cfg_ns ON cfg_ns.oid = c.cfgnamespace
JOIN pg_namespace prs_ns ON prs_ns.oid = prsnamespace
WHERE %s
ORDER BY cfgname;`, SchemaFilterClause("cfg_ns"))

	results := make([]struct {
		Schema    string
		Name      string
		ConfigOid uint32
		ParserOid uint32
		ParserFQN string
	}, 0)
	err := connection.Select(&results, query)
	utils.CheckError(err)

	parserTokens := NewParserTokenTypes()
	typeMappings := getTypeMappings(connection)

	configurations := make([]TextSearchConfiguration, 0)
	for _, row := range results {
		config := TextSearchConfiguration{}
		config.Oid = row.ConfigOid
		config.Schema = row.Schema
		config.Name = row.Name
		config.Parser = row.ParserFQN
		config.TokenToDicts = make(map[string][]string, 0)
		for _, mapping := range typeMappings[row.ConfigOid] {
			tokenTypeName := parserTokens.TokenTypeName(connection, row.ParserOid, mapping.TokenType)
			config.TokenToDicts[tokenTypeName] = append(config.TokenToDicts[tokenTypeName], mapping.Dictionary)
		}

		configurations = append(configurations, config)
	}

	return configurations
}

type ParserTokenType struct {
	TokenID uint32
	Alias   string
}

type ParserTokenTypes struct {
	forParser map[uint32][]ParserTokenType
}

func NewParserTokenTypes() *ParserTokenTypes {
	return &ParserTokenTypes{map[uint32][]ParserTokenType{}}
}

func (tokenTypes *ParserTokenTypes) TokenTypeName(connection *utils.DBConn, parserOid uint32, tokenTypeID uint32) string {
	typesForParser, ok := tokenTypes.forParser[parserOid]
	if !ok {
		typesForParser = make([]ParserTokenType, 0)
		query := fmt.Sprintf("SELECT tokid AS tokenid, alias FROM pg_catalog.ts_token_type('%d'::pg_catalog.oid)", parserOid)
		err := connection.Select(&typesForParser, query)
		utils.CheckError(err)

		tokenTypes.forParser[parserOid] = typesForParser
	}
	for _, token := range typesForParser {
		if token.TokenID == tokenTypeID {
			return token.Alias
		}
	}
	return ""
}

type TypeMapping struct {
	ConfigOid  uint32
	TokenType  uint32
	Dictionary string
}

func getTypeMappings(connection *utils.DBConn) map[uint32][]TypeMapping {
	query := `
SELECT
	mapcfg,
	maptokentype,
	mapdict::pg_catalog.regdictionary AS mapdictname
FROM pg_ts_config_map m`

	rows := make([]struct {
		MapCfg       uint32
		MapTokenType uint32
		MapDictName  string
	}, 0)
	err := connection.Select(&rows, query)
	utils.CheckError(err)

	mapping := make(map[uint32][]TypeMapping, 0)
	for _, row := range rows {
		mapping[row.MapCfg] = append(mapping[row.MapCfg], TypeMapping{
			row.MapCfg,
			row.MapTokenType,
			row.MapDictName,
		})
	}
	return mapping
}
