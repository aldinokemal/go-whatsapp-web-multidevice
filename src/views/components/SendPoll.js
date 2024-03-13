// export Vue Component
export default {
    name: 'SendPoll',
    data() {
        return {
            phone: '',
            type: 'user',
            loading: false,
            question: '',
            options: ['', ''],
            max_vote: 1,
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        openModal() {
            $('#modalSendPoll').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                window.showSuccessInfo(response)
                $('#modalSendPoll').modal('hide');
            } catch (err) {
                window.showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    question: this.question,
                    max_answer: this.max_vote,
                    options: this.options
                }
                const response = await window.http.post(`/send/poll`, payload)
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
            this.phone = '';
            this.type = 'user';
            this.question = '';
            this.options = ['', ''];
            this.max_vote = 1;
        },
        addOption() {
            this.options.push('')
        },
        deleteOption(index) {
            this.options.splice(index, 1)
        }
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Poll</div>
            <div class="description">
                Send a poll/vote with multiple options
            </div>
        </div>
    </div>
    
    <!--  Modal SendPoll  -->
    <div class="ui small modal" id="modalSendPoll">
        <i class="close icon"></i>
        <div class="header">
            Send Poll
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Type</label>
                    <select name="type" v-model="type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone / Group ID</label>
                    <input v-model="phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>
                <div class="field">
                    <label>Question</label>
                    <input v-model="question" type="text" placeholder="Please enter question"
                           aria-label="poll question">
                </div>
                <div class="field">
                    <label>Options</label>
                    <div style="display: flex; flex-direction: column; gap: 5px">
                        <div class="ui action input" :key="index" v-for="(option, index) in options">
                            <input type="text" placeholder="Option..." v-model="options[index]"
                                   aria-label="poll option">
                            <button class="ui button" @click="deleteOption(index)" type="button">
                                <i class="minus circle icon"></i>
                            </button>
                        </div>
                        <div class="field">
                            <button class="mini ui primary button" @click="addOption" type="button">
                                <i class="plus icon"></i> Option
                            </button>
                        </div>
                    </div>
                </div>
                <div class="field">
                    <label>Max Vote</label>
                    <input v-model="max_vote" type="number" placeholder="Max Vote"
                           aria-label="poll max votes" min="0">
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="handleSubmit" type="button">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
`
}