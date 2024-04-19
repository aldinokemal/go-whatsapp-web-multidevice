export default {
    name: 'AddParticipantsToGroup',
    data() {
        return {
            loading: false,
            group: '',
            participants: ['', ''],
        }
    },
    computed: {
        group_id() {
            return `${this.group}@${window.TYPEGROUP}`
        }
    },
    methods: {
        openModal() {
            $('#modalGroupAddParticipant').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        handleAddParticipant() {
            this.participants.push('')
        },
        handleDeleteParticipant(index) {
            this.participants.splice(index, 1)
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalGroupAddParticipant').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group/participants`, {
                    group_id: this.group_id,
                    // convert participant become list of string
                    participants: this.participants.filter(participant => participant !== '').map(participant => `${participant}`)
                })
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
            this.participants = ['', ''];
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Add Participants</div>
            <div class="description">
                Add multiple participants
            </div>
        </div>
    </div>
    
    <!--  Modal AccountGroup  -->
    <div class="ui small modal" id="modalGroupAddParticipant">
        <i class="close icon"></i>
        <div class="header">
            Add Participants
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
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="handleSubmit" type="button">
                Create
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}