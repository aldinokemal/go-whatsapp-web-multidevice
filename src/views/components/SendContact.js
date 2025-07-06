import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendContact',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            card_name: '',
            card_phone: '',
            loading: false,
            is_forwarded: false,
            duration: 0
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        openModal() {
            $('#modalSendContact').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isShowAttributes() {
            return this.type !== window.TYPESTATUS;
        },
        isValidForm() {
            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                return false;
            }

            if (!this.card_name.trim()) {
                return false;
            }

            if (!this.card_phone.trim()) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendContact').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            if (!this.isValidForm()) {
                return;
            }

            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    contact_name: this.card_name,
                    contact_phone: this.card_phone,
                    is_forwarded: this.is_forwarded,
                    ...(this.duration && this.duration > 0 ? {duration: this.duration} : {})
                }
                let response = await window.http.post(`/send/contact`, payload)
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
            this.card_name = '';
            this.card_phone = '';
            this.type = window.TYPEUSER;
            this.is_forwarded = false;
            this.duration = 0;
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Contact</div>
            <div class="description">
                Send contact to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendContact  -->
    <div class="ui small modal" id="modalSendContact">
        <i class="close icon"></i>
        <div class="header">
            Send Contact
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                
                <div class="field">
                    <label>Contact Name</label>
                    <input v-model="card_name" type="text" placeholder="Please enter contact name"
                           aria-label="contact name">
                </div>
                <div class="field">
                    <label>Contact Phone</label>
                    <input v-model="card_phone" type="text" placeholder="Please enter contact phone"
                           aria-label="contact phone">
                </div>
                <div class="field" v-if="isShowAttributes()">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark contact as forwarded</label>
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