import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'ChatDisappearingManager',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            timerSeconds: 86400, // Default to 24 hours
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
        timerLabel() {
            const labels = {
                0: 'Off',
                86400: '24 hours',
                604800: '7 days',
                7776000: '90 days'
            };
            return labels[this.timerSeconds] || 'Unknown';
        }
    },
    methods: {
        isValidForm() {
            const isPhoneValid = this.phone.trim().length > 0;
            return isPhoneValid;
        },
        openModal() {
            $('#modalChatDisappearing').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalChatDisappearing').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    timer_seconds: parseInt(this.timerSeconds)
                };

                const response = await window.http.post(`/chat/${this.phone_id}/disappearing`, payload);
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response?.data?.message) {
                    throw new Error(error.response.data.message);
                }
                throw error;
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.timerSeconds = 86400;
        },
    },
    template: `
    <div class="purple card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Chat</a>
            <div class="header">Disappearing Messages</div>
            <div class="description">
                Set auto-delete timer for chat messages
            </div>
        </div>
    </div>
    
    <!--  Modal ChatDisappearing  -->
    <div class="ui small modal" id="modalChatDisappearing">
        <i class="close icon"></i>
        <div class="header">
            <i class="clock outline icon"></i> Disappearing Messages
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone" :show-status="false"/>
                <div class="field">
                    <label>Timer Duration</label>
                    <select class="ui dropdown" v-model="timerSeconds">
                        <option :value="0">Off (disabled)</option>
                        <option :value="86400">24 hours</option>
                        <option :value="604800">7 days</option>
                        <option :value="7776000">90 days</option>
                    </select>
                </div>
                <div class="ui info message" v-if="timerSeconds > 0">
                    <i class="info circle icon"></i>
                    Messages will disappear after <strong>{{ timerLabel }}</strong>
                </div>
                <div class="ui warning message" v-else>
                    <i class="exclamation triangle icon"></i>
                    Disappearing messages will be <strong>disabled</strong>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'disabled': !isValidForm() || loading, 'loading': loading}"
                 @click.prevent="handleSubmit">
                {{ timerSeconds === 0 ? 'Disable Timer' : 'Set Timer' }}
                <i class="clock icon"></i>
            </button>
        </div>
    </div>
    `
}
