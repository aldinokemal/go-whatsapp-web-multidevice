export default {
    name: 'GroupMetaList',
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
                $('#modalGroupMetaList').modal('show');
                this.dtRebuild()
                showSuccessInfo("Group metadata fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        dtClear() {
            $('#group_meta_table').DataTable().destroy();
        },
        dtRebuild() {
            $('#group_meta_table').DataTable({
                "pageLength": 10,
                "reloadData": true,
            }).draw();
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/groups/metadata`)
                this.groups = response.data.results.data;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
        copyInviteLink(link) {
            if (!link) {
                showErrorInfo("No invite link available");
                return;
            }
            
            if (navigator.clipboard) {
                navigator.clipboard.writeText(link).then(() => {
                    showSuccessInfo("Invite link copied to clipboard");
                }).catch(() => {
                    this.fallbackCopy(link);
                });
            } else {
                this.fallbackCopy(link);
            }
        },
        fallbackCopy(text) {
            const tempInput = document.createElement('input');
            tempInput.value = text;
            document.body.appendChild(tempInput);
            tempInput.select();
            document.execCommand('copy');
            document.body.removeChild(tempInput);
            showSuccessInfo("Invite link copied to clipboard");
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Group Metadata</div>
            <div class="description">
                View detailed metadata for all your groups
            </div>
        </div>
    </div>
    
    <!--  Modal Group Metadata  -->
    <div class="ui large modal" id="modalGroupMetaList">
        <i class="close icon"></i>
        <div class="header">
            Group Metadata
        </div>
        <div class="content">
            <table class="ui celled table" id="group_meta_table">
                <thead>
                <tr>
                    <th>Name</th>
                    <th>Members</th>
                    <th>Admins</th>
                    <th>Role</th>
                    <th>Settings</th>
                    <th>Invite Link</th>
                </tr>
                </thead>
                <tbody v-if="groups != null">
                <tr v-for="group in groups">
                    <td>{{ group.name }}</td>
                    <td>{{ group.member_count }}</td>
                    <td>{{ group.admin_count }}</td>
                    <td>
                        <div class="ui label" :class="group.is_admin ? 'green' : 'grey'">
                            {{ group.is_admin ? 'Admin' : 'Member' }}
                        </div>
                    </td>
                    <td>
                        <div class="ui small labels">
                            <div v-if="group.locked" class="ui label">Locked</div>
                            <div v-if="group.announce" class="ui label">Announce</div>
                            <div v-if="group.ephemeral" class="ui label">Ephemeral</div>
                        </div>
                    </td>
                    <td>
                        <button v-if="group.is_admin && group.invite_link" 
                                class="ui mini primary button" 
                                @click="copyInviteLink(group.invite_link)">
                            <i class="copy icon"></i>
                            Copy Link
                        </button>
                        <span v-else class="ui grey text">—</span>
                    </td>
                </tr>
                </tbody>
            </table>
        </div>
    </div>
    `
}
