package usecase

import (
	"encoding/json"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestBuildInteractive(t *testing.T) {
	b := domainSend.NativeFlowButton{Name: "quick_reply", Params: json.RawMessage(`{"display_text":"Confirm","id":"confirm"}`)}
	req := domainSend.InteractiveRequest{Body: "Choose", Buttons: []domainSend.NativeFlowButton{b}}
	msg := buildInteractive(req)
	_, err := proto.Marshal(msg)
	require.NoError(t, err)
	i := msg.GetViewOnceMessage().GetMessage().GetInteractiveMessage()
	require.Equal(t, "Choose", i.GetBody().GetText())
	require.Equal(t, "quick_reply", i.GetNativeFlowMessage().GetButtons()[0].GetName())
	req.Buttons = nil
	req.Cards = []domainSend.InteractiveCard{{Title: "One", Body: "A", Buttons: []domainSend.NativeFlowButton{b}}, {Title: "Two", Body: "B", Buttons: []domainSend.NativeFlowButton{b}}}
	i = buildInteractive(req).GetViewOnceMessage().GetMessage().GetInteractiveMessage()
	require.Nil(t, i.GetNativeFlowMessage())
	require.Len(t, i.GetCarouselMessage().GetCards(), 2)
	require.Equal(t, "Two", i.GetCarouselMessage().GetCards()[1].GetHeader().GetTitle())
}

func TestInteractiveNodes(t *testing.T) {
	location := interactiveNodes("send_location")
	require.Equal(t, "send_location", location[0].GetChildren()[0].GetChildren()[0].Attrs["name"])
	require.Equal(t, "2", location[0].GetChildren()[0].GetChildren()[0].Attrs["v"])
	private := interactiveNodes()
	require.Len(t, private, 1)
	require.Equal(t, "biz", private[0].Tag)
	interactive := private[0].GetChildren()[0]
	require.Equal(t, "native_flow", interactive.Attrs["type"])
	require.Equal(t, "mixed", interactive.GetChildren()[0].Attrs["name"])
	require.Len(t, interactiveNodes(), 1)
}
