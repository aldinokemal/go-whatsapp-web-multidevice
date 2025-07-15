export default {
    name: 'CreateGroup',
    data() {
        return {
            loading: false,
            title: '',
            participants: ['', ''],
        }
    },
    methods: {
        openModal() {
            $('#modalGroupCreate').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            // title must contain at least one non-whitespace char
            if (!String(this.title ?? '').trim()) {
                return false;
            }

            // must have at least one valid participant
            if (
                this.participants.length < 1 ||
                this.participants.every(p => this.isEmpty(p))
            ) {
                return false;
            }

            return true;
        },
        // Helper: returns true when param is empty/blank
        isEmpty(value) {
            // If the element is an object coming from the picker, use its jid field
            const str = String(value?.jid ?? value).trim();
            return !str;
        },
        handleAddParticipant() {
            this.participants.push('')
        },
        handleDeleteParticipant(index) {
            this.participants.splice(index, 1)
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalGroupCreate').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group`, {
                    title: this.title,
                    // sanitize participants list
                    participants: this.participants
                        .filter(p => !this.isEmpty(p))
                        .map(p => `${p?.jid ?? p}`)
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
            this.title = '';
            this.participants = ['', ''];
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Create Groups</div>
            <div class="description">
                Add more friends to your group
            </div>
        </div>
    </div>
    
    <!--  Modal AccountGroup  -->
    <div class="ui small modal" id="modalGroupCreate">
        <i class="close icon"></i>
        <div class="header">
            Create Group
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group Name</label>
                    <input v-model="title" type="text"
                           placeholder="Group Name..."
                           aria-label="Group Name">
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
                            <small>You do not need to include yourself as participant. it will be automatically included.</small>
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
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                 @click.prevent="handleSubmit" type="button">
                Create
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}