// Command "generate-models" reads the model information from the "models/models.json" file,
// and generate the relevant "models/generated.go" file.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
)

// TomlTable is a map from column name to relevant type
type TomlTable map[string]string

// TomlTables is a map from table names to relevant tables.
type TomlTables map[string]TomlTable

// SnakeToGocase translates snake case to go-case.
// If export is true, the returned value has the first character in uppercase.
func SnakeToGocase(s string, export bool) string {
	parts := strings.Split(s, "_")
	result := strings.Builder{}
	for i, part := range parts {
		if i == 0 && !export {
			// Do nothing
		} else if part == "id" {
			// Special case: id => ID
			part = "ID"
		} else {
			part = strings.Title(part)
		}
		result.WriteString(part)
	}
	return result.String()
}

var t = template.New("main")

func init() {
	t.Funcs(map[string]interface{}{
		"param":     func(s string) string { return SnakeToGocase(s, false) },
		"field":     func(s string) string { return SnakeToGocase(s, true) },
		"gocase":    SnakeToGocase,
		"struct":    StructName,
		"condition": JoinCondition,
		"args":      JoinArguments,
		"marks":     Marks,
		"fkey":      ForeignKey,
	})
}

// Get the struct name from the table name.
func StructName(tableName string) string {
	// remove the trailing "s"
	tableName = tableName[:len(tableName)-1]
	// convert to Gocase
	return SnakeToGocase(tableName, true)
}

// ForeignKey translates an fk id into a struct name.
func ForeignKey(keyID string) string {
	keyID = keyID[:len(keyID)-len("_id")]
	return SnakeToGocase(keyID, true)
}

// JoinItems into a condition clause.
func JoinCondition(keys map[string]string, sep string) string {
	s := strings.Builder{}
	first := true
	for _, key := range sortKeys(keys) {
		if !first {
			s.WriteString(sep)
		}
		first = false
		s.WriteString(key + " = ?")
	}
	return s.String()
}

func sortKeys(k map[string]string) []string {
	var v []string
	for key := range k {
		v = append(v, key)
	}
	sort.Sort(sort.StringSlice(v))
	return v
}

// JoinArguments joins the key list into an argument list,
// preferrably coming from a struct.
func JoinArguments(keys map[string]string, structName string) string {
	var s []string
	for _, key := range sortKeys(keys) {
		if structName == "" {
			s = append(s, SnakeToGocase(key, false))
		} else if structName == "-" {
			s = append(s, key)
		} else {
			s = append(s, structName+"."+SnakeToGocase(key, true))
		}
	}
	return strings.Join(s, ", ")
}

// Marks generate as many question marks as needed.
func Marks(keys map[string]string) string {
	var s []string
	for range keys {
		s = append(s, "?")
	}
	return strings.Join(s, ", ")
}

// Table is a table representation.
// The rule for deriving PrimaryKeys is:
// - If the table has an "id" field, then "PrimaryKeys" is exactly "id".
// - Else, all fields ending with "id" is composed into a primary key.
type Table struct {
	Name        string
	Upsert      bool
	Fields      map[string]string
	PrimaryKeys map[string]string
	ForeignKeys map[string]string
}

// FieldsWithoutID returns a map of fields excluding the ID row.
func (t *Table) FieldsWithoutID() map[string]string {
	res := make(map[string]string)
	for field, typ := range t.Fields {
		if field != "id" {
			res[field] = typ
		}
	}
	return res
}

// TableFromToml parses out a Table from its TOML.
func TableFromToml(tables TomlTables, name string, t TomlTable) *Table {
	pks := make(map[string]string)
	fks := make(map[string]string)
	upsert := true
	for field, typ := range t {
		if strings.HasSuffix(field, "_id") {
			if _, ok := tables[field[:len(field)-len("_id")]+"s"]; ok {
				pks[field] = typ
				fks[field] = typ
			}
		}
	}
	if v, ok := t["id"]; ok {
		pks = map[string]string{"id": v}
		upsert = !(v == "int")
	}
	return &Table{
		Name:        name,
		Upsert:      upsert,
		Fields:      t,
		PrimaryKeys: pks,
		ForeignKeys: fks,
	}
}

const FileTemplate = `
// Generated by "git.nkagami.me/natsukagami/kjudge/models/generate". DO NOT EDIT.

package models

import (
    "database/sql"
    "github.com/pkg/errors"
    "git.nkagami.me/natsukagami/kjudge/db"
)

{{template "table" .}}
`

const TableTemplate = `
{{$name := .Name | struct}}
{{$tick := "` + "`" + `"}}
// {{$name}} is the struct generated from table "{{.Name}}".
type {{$name}} struct {
{{- range $field, $type := .Fields}}
    {{$field | field}} {{$type}} {{$tick}}db:"{{$field}}"{{$tick}}
{{- end}}
}

{{/* Primary Key getter */}}
{{$fn_name := print "Get" $name}}
// {{$fn_name}} gets a {{$name}} from the Database.
func {{$fn_name}}(db db.DBContext {{- range $field, $type := .PrimaryKeys -}} , {{param $field}} {{$type}} {{- end}}) (*{{$name}}, error) {
    var result {{$name}}
    if err := db.Get(&result, "SELECT * FROM {{.Name}} WHERE {{condition .PrimaryKeys " AND "}}", {{args .PrimaryKeys ""}}); err != nil {
        return nil, errors.WithStack(err)
    }
    return &result, nil
}

{{/* All foreign key getters */}}
{{range $fk, $fktype := .ForeignKeys -}}
{{ with $ }}
{{$fn_name = print "Get" ($fk | fkey) $name "s"}}
// {{$fn_name}} gets a list of {{$name}} belonging to a {{$fk | fkey}}.
func {{$fn_name}}(db db.DBContext, {{param $fk}} {{$fktype}}) ([]*{{$name}}, error) {
    var result []*{{$name}}
    if err := db.Select(&result, "SELECT * FROM {{.Name}} WHERE {{$fk}} = ?", {{param $fk}}); err != nil {
        return nil, errors.WithStack(err)
    }
    return result, nil
}
{{end}}
{{end}}

{{/* Update and Insert */}}
{{if .Upsert}}
{{template "upsert" .}}
{{else}}
{{template "update_or_insert" .}}
{{end}}

{{/* Delete */}}
// Delete deletes the {{$name}} from the Database.
func (r *{{$name}}) Delete(db db.DBContext) error {
    _, err := db.Exec("DELETE FROM {{.Name}} WHERE {{condition .PrimaryKeys " AND "}}", {{args .PrimaryKeys "r"}})
    return errors.WithStack(err)
}
`
const UpsertTemplate = `
{{$name := .Name | struct}}
{{$tick := "` + "`" + `"}}
// Write writes the change to the Database. This happens as an UPSERT statement.
func (r *{{$name}}) Write(db db.DBContext) error {
    if err := r.Verify(); err != nil {
        return err
    }
    _, err := db.Exec("INSERT INTO {{.Name}}({{args .Fields "-"}}) VALUES ({{marks .Fields}}) ON CONFLICT ({{args .PrimaryKeys "-"}}) DO UPDATE SET {{condition .Fields ", "}}",
                        {{args .Fields "r"}}, {{args .Fields "r"}})
    return errors.WithStack(err)
}
`

const UpdateOrInsertTemplate = `
{{$name := .Name | struct}}
{{$tick := "` + "`" + `"}}
// Write writes the change to the Database.
// If the ID of the {{$name}} is 0, then an INSERT is performed. Else, an UPDATE is triggered.
func (r *{{$name}}) Write(db db.DBContext) error {
    if err := r.Verify(); err != nil {
        return err
    }
    {{if eq (index .Fields "id") "int"}}
    if r.ID == 0 {
        {{ $fields := .FieldsWithoutID }}
        res, err := db.Exec("INSERT INTO {{.Name}}({{args $fields "-"}}) VALUES ({{marks $fields}})", {{args $fields "r"}})
        if err != nil {
            return errors.WithStack(err)
        }
        id, err := res.LastInsertId()
        if err != nil {
            return errors.WithStack(err)
        }
        r.ID = int(id)
        return nil
    }
    {{end}}
    _, err := db.Exec("UPDATE {{.Name}} SET {{condition .Fields ", "}} WHERE {{condition .PrimaryKeys " AND "}}",
                      {{args .Fields "r"}}, {{args .PrimaryKeys "r"}})
    return errors.WithStack(err)
}
`

func init() {
	template.Must(t.New("upsert").Parse(UpsertTemplate))
	template.Must(t.New("update_or_insert").Parse(UpdateOrInsertTemplate))
	template.Must(t.New("table").Parse(TableTemplate))
	template.Must(t.Parse(FileTemplate))
}

func main() {
	var tables TomlTables
	if _, err := toml.DecodeFile("models/models.toml", &tables); err != nil {
		log.Fatal(err)
	}
	if err := exec.Command("rm", "-v", "models/*_generated.go").Run(); err != nil {
		// log.Fatal(err)
	}
	for name, fields := range tables {
		table := TableFromToml(tables, name, fields)
		filename := fmt.Sprintf("models/%s_generated.go", name)
		log.Printf("> Handling %s\n", filename)
		f, err := os.Create(filename)
		if err != nil {
			log.Fatal(err)
		}
		if err := t.Execute(f, table); err != nil {
			log.Fatal(err)
		}
		if err := f.Close(); err != nil {
			log.Fatal(err)
		}
		if err := exec.Command("go", "fmt", filename).Run(); err != nil {
			log.Fatal(err)
		}
		if err := exec.Command("goimports", "-w", filename).Run(); err != nil {
			log.Fatal(err)
		}
		log.Printf("Generated code for table %s to %s\n", name, filename)
	}
}
