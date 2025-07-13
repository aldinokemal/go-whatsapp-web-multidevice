// export Vue Component
import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendPoll',
    components: {
        FormRecipient
    },
    data() {
        return {
            phone: '',
            type: window.TYPEUSER,
            loading: false,
            question: '',
            options: ['', ''],
            max_answer: 1,
            duration: 0,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
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
        isValidForm() {
            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                return false;
            }

            if (!this.question.trim()) {
                return false;
            }
            
            if (this.options.some(option => option.trim() === '')) {
                return false;
            }

            if (this.max_answer < 1 || this.max_answer > this.options.length) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

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
                    options: this.options,
                    max_answer: this.max_answer,
                    ...(this.duration && this.duration > 0 ? {duration: this.duration} : {})
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
            this.type = window.TYPEUSER;
            this.question = '';
            this.options = ['', ''];
            this.max_answer = 1;
            this.duration = 0;
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
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                
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
                    <label>Max Answers Allowed</label>
                    <input v-model.number="max_answer" type="number" placeholder="Maximum answers per user" 
                           aria-label="poll max answers" min="1" max="50">
                    <div class="ui pointing label">
                        How many options each user can select
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
`
}