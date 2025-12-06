export default {
    name: 'ExportGroupParticipants',
    data() {
        return {
            loading: false,
            groupId: ''
        }
    },
    methods: {
        async exportParticipants() {
            if (!this.groupId.trim()) {
                showErrorInfo('Please enter a group ID');
                return;
            }

            this.loading = true;
            try {
                const response = await window.http.get(`/group/export-participants?group_id=${this.groupId}`, {
                    responseType: 'blob'
                });
                const url = window.URL.createObjectURL(new Blob([response.data]));
                const link = document.createElement('a');
                link.href = url;
                link.setAttribute('download', `group-participants-${this.groupId}.csv`);
                document.body.appendChild(link);
                link.click();
                document.body.removeChild(link);
                window.URL.revokeObjectURL(url);
                showSuccessInfo('Participants exported successfully as CSV');
            } catch (error) {
                showErrorInfo(error.response?.data?.message || error.message);
            } finally {
                this.loading = false;
            }
        }
    },
    template: `
        <div class="column">
            <div class="ui fluid card">
                <div class="content">
                    <div class="header">
                        <i class="download icon"></i>
                        Export Group Participants
                    </div>
                    <div class="description">
                        Export group participants as CSV with name and phone number in E.164 format
                    </div>
                </div>
                <div class="content">
                    <div class="ui form">
                        <div class="field">
                            <label>Group ID</label>
                            <div class="ui input">
                                <input type="text" v-model="groupId" placeholder="120363XXXXXXXX@g.us">
                            </div>
                        </div>
                    </div>
                </div>
                <div class="extra content">
                    <button class="ui primary button fluid" 
                            :class="{ 'loading': loading }" 
                            @click="exportParticipants"
                            :disabled="loading">
                        <i class="download icon"></i>
                        Export as CSV
                    </button>
                </div>
            </div>
        </div>
    `
}
