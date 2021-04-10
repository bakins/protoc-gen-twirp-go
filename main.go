package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"os"
	"text/template"

	"google.golang.org/protobuf/compiler/protogen"
)

//go:embed *.tmpl
var templates embed.FS

func main() {
	var flags flag.FlagSet

	protogen.Options{
		ParamFunc: flags.Set,
	}.Run(func(gen *protogen.Plugin) error {
		for _, f := range gen.Files {
			if f.Generate {
				generateFile(gen, f)
			}
		}
		return nil
	})
}

type templatePackage struct {
	Name     string
	Package  string
	Services []templateService
}

type templateService struct {
	Name    string
	GoName  string
	Methods []templateMethod
}

type templateMethod struct {
	Name   string
	GoName string
	Input  string
	Output string
}

func exitError(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func generateFile(gen *protogen.Plugin, file *protogen.File) {
	if len(file.Services) == 0 {
		return
	}

	data, err := templates.ReadFile("twirp.go.tmpl")
	if err != nil {
		exitError(err)
	}

	tmpl, err := template.New("twirp.go.tmpl").Parse(string(data))
	if err != nil {
		exitError(err)
	}

	_ = tmpl
	filename := file.GeneratedFilenamePrefix + "_twirp_service.pb.go"
	g := gen.NewGeneratedFile(filename, file.GoImportPath)

	_ = g

	tp := templatePackage{
		Name:    string(file.Desc.FullName()),
		Package: string(file.GoPackageName),
	}

	for _, service := range file.Services {
		if len(service.Methods) == 0 {
			continue
		}

		s := templateService{
			Name:   string(service.Desc.Name()),
			GoName: service.GoName,
		}

		for _, method := range service.Methods {
			m := templateMethod{
				Name:   string(method.Desc.Name()),
				GoName: method.GoName,
				Input:  g.QualifiedGoIdent(method.Input.GoIdent),
				Output: g.QualifiedGoIdent(method.Output.GoIdent),
			}

			s.Methods = append(s.Methods, m)
		}

		tp.Services = append(tp.Services, s)
	}

	if len(tp.Services) == 0 {
		return
	}

	var buff bytes.Buffer
	if err := tmpl.Execute(&buff, &tp); err != nil {
		exitError(err)
	}

	g.P(buff.String())
}
