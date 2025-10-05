import GroupListParticipants from "./GroupListParticipants.js";

export default {
    name: 'ListGroup',
    components: { GroupListParticipants },
    props: ['connected'],
    data() {
        return {
            groups: [],
            selectedGroupId: null,
            requestedMembers: [],
            loadingRequestedMembers: false,
            processingMember: null,
        }
    },
    computed: {
        currentUserId() {
            if (!this.connected || this.connected.length === 0) return null;
            const device = this.connected[0].device;
            return device.split('@')[0].split(':')[0];
        }
    },
    methods: {
        async openModal() {
            try {
                this.dtClear()
                await this.submitApi();
                $('#modalGroupList').modal('show');
                this.dtRebuild()
                showSuccessInfo("Groups fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        dtClear() {
            $('#account_groups_table').DataTable().destroy();
        },
        dtRebuild() {
            $('#account_groups_table').DataTable({
                "pageLength": 100,
                "reloadData": true,
            }).draw();
        },
        async handleLeaveGroup(group_id) {
            try {
                const ok = confirm("Are you sure to leave this group?");
                if (!ok) return;

                await this.leaveGroupApi(group_id);
                this.dtClear()
                await this.submitApi();
                this.dtRebuild()
                showSuccessInfo("Group left")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async leaveGroupApi(group_id) {
            try {
                let payload = new FormData();
                payload.append("group_id", group_id)
                await window.http.post(`/group/leave`, payload)
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);

            }
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/groups`)
                this.groups = response.data.results.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
        formatDate: function (value) {
            if (!value) return ''
            return moment(value).format('LLL');
        },
        isAdmin(group) {
            // Check if current user is the owner
            const owner = group.OwnerJID.split('@')[0];
            if (owner === this.currentUserId) {
                return true;
            }
            
            // Check if current user is an admin in participants
            const currentUserJID = `${this.currentUserId}@s.whatsapp.net`;
            const participant = group.Participants.find(p => p.PhoneNumber === currentUserJID);
            return participant && participant.IsAdmin;
        },
        async handleSeeRequestedMember(group_id) {
            this.selectedGroupId = group_id;
            this.loadingRequestedMembers = true;
            this.requestedMembers = [];
            
            try {
                const response = await window.http.get(`/group/participant-requests?group_id=${group_id}`);
                this.requestedMembers = response.data.results || [];
                this.loadingRequestedMembers = false;
                $('#modalRequestedMembers').modal('show');
            } catch (error) {
                this.loadingRequestedMembers = false;
                let errorMessage = "Failed to fetch requested members";
                if (error.response) {
                    errorMessage = error.response.data.message || errorMessage;
                }
                showErrorInfo(errorMessage);
            }
        },
        async handleSeeParticipants(group) {
            if (!group || !group.JID) return;

            this.selectedGroupId = group.JID;
            $('#modalGroupList').modal('hide');

            try {
                await this.$refs.participantsModal.open(group);
            } catch (error) {
                const errorMessage = error?.message || 'Failed to fetch participants';
                showErrorInfo(errorMessage);
                $('#modalGroupList').modal('show');
            }
        },
        handleExportParticipants(group) {
            if (!group || !group.JID) return;

            const baseURL = (window.http && window.http.defaults && window.http.defaults.baseURL) ? window.http.defaults.baseURL : '';
            const exportUrl = `${baseURL}/group/participants/export?group_id=${encodeURIComponent(group.JID)}`;
            window.open(exportUrl, '_blank');
        },
        formatJID(jid) {
            return jid ? jid.split('@')[0] : '';
        },
        closeRequestedMembersModal() {
            $('#modalRequestedMembers').modal('hide');
            // open modal again
            this.openModal();
        },
        handleParticipantsClosed() {
            $('#modalGroupList').modal('show');
        },
        async handleProcessRequest(member, action) {
            if (!this.selectedGroupId || !member) return;

            const actionText = action === 'approve' ? 'approve' : 'reject';
            const confirmMsg = `Are you sure you want to ${actionText} this member request?`;
            const ok = confirm(confirmMsg);
            if (!ok) return;

            try {
                this.processingMember = member.jid;

                const payload = {
                    group_id: this.selectedGroupId,
                    participants: [this.formatJID(member.jid)]
                };

                await window.http.post(`/group/participant-requests/${action}`, payload);

                // Remove the processed member from the list
                this.requestedMembers = this.requestedMembers.filter(m => m.jid !== member.jid);

                showSuccessInfo(`Member request ${actionText}d`);
                this.processingMember = null;
            } catch (error) {
                this.processingMember = null;
                let errorMessage = `Failed to ${actionText} member request`;
                if (error.response) {
                    errorMessage = error.response.data.message || errorMessage;
                }
                showErrorInfo(errorMessage);
            }
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">List Groups</div>
            <div class="description">
                Display all your groups
            </div>
        </div>
    </div>
    
    <!--  Modal AccountGroup  -->
    <div class="ui large modal" id="modalGroupList">
        <i class="close icon"></i>
        <div class="header">
            My Group List
        </div>
        <div class="content">
            <table class="ui celled table" id="account_groups_table">
                <thead>
                <tr>
                    <th>Group ID</th>
                    <th>Name</th>
                    <th>Participants</th>
                    <th>Created At</th>
                    <th>Action</th>
                </tr>
                </thead>
                <tbody v-if="groups != null">
                <tr v-for="g in groups">
                    <td>{{ g.JID.split('@')[0] }}</td>
                    <td>{{ g.Name }}</td>
                    <td>{{ g.Participants.length }}</td>
                    <td>{{ formatDate(g.GroupCreated) }}</td>
                    <td>
                        <div style="display: flex; gap: 8px; align-items: center;">
                            <button class="ui blue tiny button" @click="handleSeeParticipants(g)">Participants</button>
                            <button class="ui grey tiny button" @click="handleExportParticipants(g)">Export CSV</button>
                            <button v-if="isAdmin(g)" class="ui green tiny button" @click="handleSeeRequestedMember(g.JID)">Requested Members</button>
                            <button class="ui red tiny button" @click="handleLeaveGroup(g.JID)">Leave</button>
                        </div>
                    </td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>

    <group-list-participants ref="participantsModal" @closed="handleParticipantsClosed"></group-list-participants>

    <!-- Requested Members Modal -->
    <div class="ui modal" id="modalRequestedMembers">
        <i class="close icon"></i>
        <div class="header">
            Requested Group Members
        </div>
        <div class="content">
            <div v-if="loadingRequestedMembers" class="ui active centered inline loader"></div>
            
            <div v-else-if="requestedMembers.length === 0" class="ui info message">
                <div class="header">No Requested Members</div>
                <p>There are no pending member requests for this group.</p>
            </div>
            
            <table v-else class="ui celled table">
                <thead>
                    <tr>
                        <th>User ID</th>
                        <th>Request Time</th>
                        <th>Action</th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="member in requestedMembers" :key="member.jid">
                        <td>{{ formatJID(member.jid) }}</td>
                        <td>{{ formatDate(member.requested_at) }}</td>
                        <td>
                            <div class="ui mini buttons">
                                <button class="ui green button" 
                                        @click="handleProcessRequest(member, 'approve')"
                                        :disabled="processingMember === member.jid">
                                    <i v-if="processingMember === member.jid" class="spinner loading icon"></i>
                                    Approve
                                </button>
                                <div class="or"></div>
                                <button class="ui red button" 
                                        @click="handleProcessRequest(member, 'reject')"
                                        :disabled="processingMember === member.jid">
                                    <i v-if="processingMember === member.jid" class="spinner loading icon"></i>
                                    Reject
                                </button>
                            </div>
                        </td>
                    </tr>
                </tbody>
            </table>
        </div>
        <div class="actions">
            <div class="ui button" @click="closeRequestedMembersModal">Close</div>
        </div>
    </div>
    `
}
