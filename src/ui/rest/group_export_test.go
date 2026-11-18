package rest

import (
	"context"
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type exportGroupStub struct {
	domainGroup.IGroupUsecase
	result domainGroup.GetGroupParticipantsResponse
}

func (s exportGroupStub) GetGroupParticipants(context.Context, domainGroup.GetGroupParticipantsRequest) (domainGroup.GetGroupParticipantsResponse, error) {
	return s.result, nil
}

func TestExportParticipantsQuotesFormulaCells(t *testing.T) {
	app := fiber.New()
	controller := Group{Service: exportGroupStub{result: domainGroup.GetGroupParticipantsResponse{
		GroupID: "120363@g.us",
		Participants: []domainGroup.GroupParticipant{
			{
				JID:         "1555@s.whatsapp.net",
				PhoneNumber: "+62811",
				LID:         "1555@lid",
				DisplayName: `=HYPERLINK("http://evil.example","click")`,
				IsAdmin:     true,
			},
			{JID: "\t=1", PhoneNumber: "-1", LID: "@evil", DisplayName: "+SUM(A1)"},
			{JID: "2@s.whatsapp.net", PhoneNumber: "62812", LID: "2@lid", DisplayName: "Alice\r=cmd"},
			{JID: "3@s.whatsapp.net", DisplayName: " =HYPERLINK(\"http://evil.example\",\"x\")"},
			{JID: "4@s.whatsapp.net", DisplayName: "\t+SUM(A1)"},
			{JID: "\t－1", PhoneNumber: "＋62813", LID: "＠evil", DisplayName: "＝HYPERLINK(\"http://evil.example\",\"x\")"},
			{JID: "5@s.whatsapp.net", DisplayName: "Bob;=1+1;x"},
			{JID: "6@s.whatsapp.net", DisplayName: "Tab\t+1, @x;; -2"},
		},
	}}}
	app.Get("/group/participants/export", controller.ExportParticipants)

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/group/participants/export?group_id=120363@g.us", nil))
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	records, err := csv.NewReader(resp.Body).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 9)
	require.Equal(t, `'=HYPERLINK("http://evil.example","click")`, records[1][3])
	require.Equal(t, "'+62811", records[1][1])
	require.Equal(t, "admin", records[1][4])
	require.Equal(t, "'\t'=1", records[2][0])
	require.Equal(t, "'-1", records[2][1])
	require.Equal(t, "'@evil", records[2][2])
	require.Equal(t, "'+SUM(A1)", records[2][3])
	require.Equal(t, "Alice\r'=cmd", records[3][3])
	require.True(t, strings.HasPrefix(records[3][3], "Alice"))
	require.Equal(t, "' =HYPERLINK(\"http://evil.example\",\"x\")", records[4][3])
	require.Equal(t, "'\t'+SUM(A1)", records[5][3])
	require.Equal(t, "'\t'－1", records[6][0])
	require.Equal(t, "'＋62813", records[6][1])
	require.Equal(t, "'＠evil", records[6][2])
	require.Equal(t, "'＝HYPERLINK(\"http://evil.example\",\"x\")", records[6][3])
	require.Equal(t, "Bob;'=1+1;x", records[7][3])
	require.Equal(t, "Tab\t'+1,' @x;;' -2", records[8][3])
}
