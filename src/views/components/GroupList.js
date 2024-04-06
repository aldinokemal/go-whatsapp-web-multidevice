export default {
    name: 'ListGroup',
    data() {
        return {
            groups: []
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
    <div class="ui small modal" id="modalGroupList">
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
                        <button class="ui red tiny button" @click="handleLeaveGroup(g.JID)">Leave</button>
                    </td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>
    `
}