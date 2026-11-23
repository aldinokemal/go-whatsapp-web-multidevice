package mcpauth

import (
	"context"
	"fmt"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// MyDevicesTool is the tool's name.
const MyDevicesTool = "wtf_my_devices"

type myDevice struct {
	DeviceID string `json:"device_id"`
	Phone    string `json:"phone,omitempty"`
	Name     string `json:"name,omitempty"`
	State    string `json:"state"`
}

// Tools adds wtf_my_devices: the caller's own WhatsApp numbers, through the
// owner-scoped device usecase (another person's numbers never appear).
func Tools(devices domainDevice.IDeviceUsecase) func(s *server.MCPServer) {
	return func(s *server.MCPServer) {
		tool := mcpg.NewTool(MyDevicesTool,
			mcpg.WithDescription("Lists the WhatsApp numbers linked to your account: each one's device_id, phone number, WhatsApp name and state "+
				"(logged_in, disconnected or another state). When you have more than one number, pass the chosen device_id "+
				"as the device_id argument of the other WhatsApp tools; with exactly one, they use it by default."),
			mcpg.WithTitleAnnotation("My WhatsApp numbers"),
			mcpg.WithReadOnlyHintAnnotation(true),
			mcpg.WithDestructiveHintAnnotation(false),
			mcpg.WithIdempotentHintAnnotation(true),
			mcpg.WithOpenWorldHintAnnotation(false),
		)
		s.AddTool(tool, func(ctx context.Context, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := devices.ListDevices(ctx)
			if err != nil {
				return mcpg.NewToolResultError(err.Error()), nil
			}
			items := make([]myDevice, 0, len(list))
			for _, d := range list {
				items = append(items, myDevice{DeviceID: d.ID, Phone: d.PhoneNumber, Name: d.DisplayName, State: string(d.State)})
			}
			structured := map[string]any{"total": len(items), "items": items}
			return mcpg.NewToolResultStructured(structured, fmt.Sprintf("%d WhatsApp number(s) linked", len(items))), nil
		})
	}
}
