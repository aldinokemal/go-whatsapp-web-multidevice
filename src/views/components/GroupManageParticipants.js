export default {
    name: 'ManageGroupParticipants',
    data() {
        return {
            loading: false,
            group: '',
            action: 'add', // add, remove, promote, demote
            participants: ['', ''],
        };
    },
    computed: {
        group_id() {
            return `${this.group}${window.TYPEGROUP}`;
        },
    },
    methods: {
        openModal() {
            $('#modalGroupAddParticipant').modal({
                onApprove: function() {
                    return false;
                },
            }).modal('show');
        },
        isValidForm() {
            if (
                this.participants.length < 1 ||
                this.participants.every(p => this.isEmpty(p))
            ) {
                return false;
            }

            return true;
        },
        // Helper to determine if participant value is empty
        isEmpty(value) {
            const str = String(value?.jid ?? value).trim();
            return !str;
        },
        handleAddParticipant() {
            this.participants.push('');
        },
        handleDeleteParticipant(index) {
            this.participants.splice(index, 1);
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                let response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalGroupAddParticipant').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    group_id: this.group_id,
                    // convert participant become list of string
                    participants: this.participants
                        .filter(p => !this.isEmpty(p))
                        .map(p => `${p?.jid ?? p}`),
                };

                let response;
                switch (this.action) {
                    case 'add':
                        response = await window.http.post(`/group/participants`, payload);
                        break;
                    case 'remove':
                        response = await window.http.post(`/group/participants/remove`, payload);
                        break;
                    case 'promote':
                        response = await window.http.post(`/group/participants/promote`, payload);
                        break;
                    case 'demote':
                        response = await window.http.post(`/group/participants/demote`, payload);
                        break;
                }

                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.group = '';
            this.action = 'add';
            this.participants = ['', ''];
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Manage Participants</div>
            <div class="description">
                Add/Remove/Promote/Demote Participants
            </div>
        </div>
    </div>
    
    <!--  Modal AccountGroup  -->
    <div class="ui small modal" id="modalGroupAddParticipant">
        <i class="close icon"></i>
        <div class="header">
            Manage Participants
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group ID</label>
                    <input v-model="group" type="text"
                           placeholder="12036322888236XXXX..."
                           aria-label="Group Name">
                    <input :value="group_id" disabled aria-label="whatsapp_id">
                </div>
                
                <div class="field">
                    <label>Participants</label>
                    <div style="display: flex; flex-direction: column; gap: 5px">
                        <div class="ui action input" :key="index" v-for="(participant, index) in participants">
                            <input type="number" placeholder="Phone Int Number (6289...)" v-model="participants[index]"
                                   aria-label="list participant">
                            <button class="ui button" @click="handleDeleteParticipant(index)" type="button">
                                <i class="minus circle icon"></i>
                            </button>
                        </div>
                        <div class="field" style="display: flex; flex-direction: column; gap: 3px">
                            <div>
                                <button class="mini ui primary button" @click="handleAddParticipant" type="button">
                                    <i class="plus icon"></i> Option
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
                
                <div class="field">
                    <label>Action</label>
                    <select v-model="action" class="ui dropdown" aria-label="Action">
                        <option value="add">Add to group</option>
                        <option value="remove">Remove from group</option>
                        <option value="promote">Promote to admin</option>
                        <option value="demote">Demote from admin</option>
                    </select>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                 @click.prevent="handleSubmit" type="button">
                Submit
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `,
};