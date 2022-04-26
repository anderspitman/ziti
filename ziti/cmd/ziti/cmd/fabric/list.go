/*
	Copyright NetFoundry, Inc.

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package fabric

import (
	"fmt"
	fabric_rest_client "github.com/openziti/fabric/rest_client"
	"github.com/openziti/fabric/rest_client/link"
	"github.com/openziti/fabric/rest_client/router"
	"github.com/openziti/fabric/rest_client/service"
	"github.com/openziti/fabric/rest_client/terminator"
	"github.com/openziti/fabric/rest_model"
	"github.com/openziti/foundation/util/stringz"
	"strings"

	"github.com/Jeffail/gabs"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/openziti/ziti/ziti/cmd/ziti/cmd/api"
	"github.com/openziti/ziti/ziti/cmd/ziti/cmd/common"
	cmdhelper "github.com/openziti/ziti/ziti/cmd/ziti/cmd/helpers"
	"github.com/openziti/ziti/ziti/cmd/ziti/util"
	"github.com/spf13/cobra"
)

// newListCmd creates a command object for the "controller list" command
func newListCmd(p common.OptionsProvider) *cobra.Command {
	listCmd := &cobra.Command{
		Use:     "list",
		Short:   "Lists various entities managed by the Ziti Controller",
		Aliases: []string{"ls"},
		Run: func(cmd *cobra.Command, args []string) {
			err := cmd.Help()
			cmdhelper.CheckErr(err)
		},
	}

	newOptions := func() *api.Options {
		return &api.Options{CommonOptions: p()}
	}

	listCmd.AddCommand(newListCmdForEntityType("circuits", runListCircuits, newOptions()))
	listCmd.AddCommand(newListCmdForEntityType("links", runListLinks, newOptions()))
	listCmd.AddCommand(newListCmdForEntityType("routers", runListRouters, newOptions()))
	listCmd.AddCommand(newListCmdForEntityType("services", runListServices, newOptions()))
	listCmd.AddCommand(newListCmdForEntityType("terminators", runListTerminators, newOptions()))

	return listCmd
}

func listEntitiesWithOptions(entityType string, options *api.Options) ([]*gabs.Container, *api.Paging, error) {
	return api.ListEntitiesWithOptions(util.FabricAPI, entityType, options)
}

type listCommandRunner func(*api.Options) error

// newListCmdForEntityType creates the list command for the given entity type
func newListCmdForEntityType(entityType string, command listCommandRunner, options *api.Options, aliases ...string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     entityType + " <filter>?",
		Short:   "lists " + entityType + " managed by the Ziti Controller",
		Args:    cobra.MaximumNArgs(1),
		Aliases: aliases,
		Run: func(cmd *cobra.Command, args []string) {
			options.Cmd = cmd
			options.Args = args
			err := command(options)
			cmdhelper.CheckErr(err)
		},
		SuggestFor: []string{},
	}

	// allow interspersing positional args and flags
	cmd.Flags().SetInterspersed(true)
	cmd.Flags().BoolVar(&options.OutputCSV, "csv", false, "Output CSV instead of a formatted table")
	options.AddCommonFlags(cmd)

	return cmd
}

func runListCircuits(o *api.Options) error {
	children, pagingInfo, err := listEntitiesWithOptions("circuits", o)
	if err != nil {
		return err
	}
	return outputCircuits(o, children, pagingInfo)
}

func outputCircuits(o *api.Options, children []*gabs.Container, pagingInfo *api.Paging) error {
	if o.OutputJSONResponse {
		return nil
	}

	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "Client", "Service", "Path"})

	for _, entity := range children {
		id := api.GetJsonString(entity, "id")
		client := api.GetJsonString(entity, "clientId")
		serviceName := api.GetJsonString(entity, "service.name")

		path := strings.Builder{}

		nodes, err := getEntityRef(entity.Path("path.nodes"))
		if err != nil {
			return err
		}

		links, err := getEntityRef(entity.Path("path.links"))
		if err != nil {
			return err
		}

		if len(nodes) > 0 {
			path.WriteString("r/")
			path.WriteString(nodes[0].name)
			for idx, node := range nodes[1:] {
				link := links[idx]
				path.WriteString(" -> l/")
				path.WriteString(link.id)
				path.WriteString(" -> r/")
				path.WriteString(node.name)
			}
		}

		t.AppendRow(table.Row{id, client, serviceName, path.String()})
	}

	api.RenderTable(o, t, pagingInfo)

	return nil
}

type entityRef struct {
	id   string
	name string
}

func getEntityRef(c *gabs.Container) ([]*entityRef, error) {
	if c == nil || c.Data() == nil {
		return nil, nil
	}
	children, err := c.Children()
	if err != nil {
		return nil, err
	}

	var result []*entityRef

	for _, child := range children {
		id := api.GetJsonString(child, "id")
		name := api.GetJsonString(child, "name")
		result = append(result, &entityRef{
			id:   id,
			name: name,
		})
	}
	return result, nil
}

func runListLinks(o *api.Options) error {
	return WithFabricClient(o, func(client *fabric_rest_client.ZitiFabric) error {
		result, err := client.Link.ListLinks(&link.ListLinksParams{
			//Filter:  o.GetFilter(),
			Context: o.GetContext(),
		})
		return outputResult(result, err, o, outputLinks)
	})
}

func outputLinks(o *api.Options, results *link.ListLinksOK) error {
	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	columnConfigs := []table.ColumnConfig{
		{Number: 5, Align: text.AlignRight},
		{Number: 6, Align: text.AlignRight},
		{Number: 8, Align: text.AlignRight},
	}
	t.SetColumnConfigs(columnConfigs)
	t.AppendHeader(table.Row{"ID", "Dialer", "Acceptor", "Static Cost", "Src Latency", "Dst Latency", "State", "Status", "Full Cost"})

	for _, entity := range results.Payload.Data {
		id := valOrDefault(entity.ID)
		srcRouter := entity.SourceRouter.Name
		dstRouter := entity.DestRouter.Name
		staticCost := valOrDefault(entity.StaticCost)
		srcLatency := float64(valOrDefault(entity.SourceLatency)) / 1_000_000
		dstLatency := float64(valOrDefault(entity.DestLatency)) / 1_000_000
		state := valOrDefault(entity.State)
		down := valOrDefault(entity.Down)
		cost := valOrDefault(entity.Cost)

		status := "up"
		if down {
			status = "down"
		}

		t.AppendRow(table.Row{id, srcRouter, dstRouter, staticCost,
			fmt.Sprintf("%.1fms", srcLatency),
			fmt.Sprintf("%.1fms", dstLatency),
			state, status, cost})
	}

	api.RenderTable(o, t, getPaging(results.Payload.Meta))

	return nil
}

func runListTerminators(o *api.Options) error {
	return WithFabricClient(o, func(client *fabric_rest_client.ZitiFabric) error {
		result, err := client.Terminator.ListTerminators(&terminator.ListTerminatorsParams{
			Filter:  o.GetFilter(),
			Context: o.GetContext(),
		})
		return outputResult(result, err, o, outputTerminators)
	})
}

func outputTerminators(o *api.Options, result *terminator.ListTerminatorsOK) error {
	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "Service", "Router", "Binding", "Address", "Identity", "Cost", "Precedence", "Dynamic Cost"})

	for _, entity := range result.Payload.Data {
		id := valOrDefault(entity.ID)
		serviceName := entity.Service.Name
		routerName := entity.Router.Name
		binding := valOrDefault(entity.Binding)
		address := valOrDefault(entity.Address)
		identity := valOrDefault(entity.Identity)
		staticCost := valOrDefault(entity.Cost)
		precedence := valOrDefault(entity.Precedence)
		dynamicCost := valOrDefault(entity.DynamicCost)

		t.AppendRow(table.Row{id, serviceName, routerName, binding, address, identity, staticCost, precedence, dynamicCost})
	}

	api.RenderTable(o, t, getPaging(result.Payload.Meta))
	return nil
}

func runListServices(o *api.Options) error {
	return WithFabricClient(o, func(client *fabric_rest_client.ZitiFabric) error {
		result, err := client.Service.ListServices(&service.ListServicesParams{
			Filter:  o.GetFilter(),
			Context: o.GetContext(),
		})
		return outputResult(result, err, o, outputServices)
	})
}

func outputServices(o *api.Options, result *service.ListServicesOK) error {
	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "Name", "Terminator Strategy"})

	for _, entity := range result.Payload.Data {
		t.AppendRow(table.Row{
			valOrDefault(entity.ID),
			valOrDefault(entity.Name),
			valOrDefault(entity.TerminatorStrategy),
		})
	}

	api.RenderTable(o, t, getPaging(result.Payload.Meta))

	return nil
}

func runListRouters(o *api.Options) error {
	return WithFabricClient(o, func(client *fabric_rest_client.ZitiFabric) error {
		result, err := client.Router.ListRouters(&router.ListRoutersParams{
			Filter:  o.GetFilter(),
			Context: o.GetContext(),
		})
		return outputResult(result, err, o, outputRouters)
	})
}

func outputRouters(o *api.Options, result *router.ListRoutersOK) error {
	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.AppendHeader(table.Row{"ID", "Name", "Online", "Cost", "No Traversal", "Version", "Listeners"})

	for _, entity := range result.Payload.Data {
		var version string
		if versionInfo := entity.VersionInfo; versionInfo != nil {
			version = fmt.Sprintf("%v on %v/%v", versionInfo.Version, versionInfo.Os, versionInfo.Arch)
		}
		var listeners []string
		for idx, listenerAddr := range entity.ListenerAddresses {
			addr := stringz.OrEmpty(listenerAddr.Address)
			listeners = append(listeners, fmt.Sprintf("%v: %v", idx+1, addr))
		}
		t.AppendRow(table.Row{
			valOrDefault(entity.ID),
			valOrDefault(entity.Name),
			valOrDefault(entity.Connected),
			valOrDefault(entity.Cost),
			valOrDefault(entity.NoTraversal),
			version,
			strings.Join(listeners, "\n")})
	}

	api.RenderTable(o, t, getPaging(result.Payload.Meta))

	return nil
}

func getPaging(meta *rest_model.Meta) *api.Paging {
	return &api.Paging{
		Limit:  *meta.Pagination.Limit,
		Offset: *meta.Pagination.Offset,
		Count:  *meta.Pagination.TotalCount,
	}
}

func outputResult[T any](val T, err error, o *api.Options, f func(o *api.Options, val T) error) error {
	if err != nil {
		return err
	}
	if o.OutputJSONResponse {
		return nil
	}
	return f(o, val)
}

func valOrDefault[V any, T *V](val T) V {
	var result V
	if val != nil {
		result = *val
	}
	return result
}
