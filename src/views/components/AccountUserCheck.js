import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'AccountUserCheck',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            isOnWhatsApp: null,
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        async openModal() {
            this.handleReset();
            $('#modalUserCheck').modal('show');
        },
        isValidForm() {
            return this.phone.trim() !== '';
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                await this.submitApi();
                showSuccessInfo("Check completed")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/user/check?phone=${this.phone_id}`)
                this.isOnWhatsApp = response.data.results.is_on_whatsapp;
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
            this.isOnWhatsApp = null;
            this.type = window.TYPEUSER;
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
            <a class="ui olive right ribbon label">Account</a>
            <div class="header">User Check</div>
            <div class="description">
                Check if a user is on WhatsApp
            </div>
        </div>
    </div>
    
    <div class="ui small modal" id="modalUserCheck">
        <i class="close icon"></i>
        <div class="header">
            Check if User is on WhatsApp
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !this.isValidForm() || this.loading}"
                        @click.prevent="handleSubmit">
                    Check
                </button>
            </form>

            <div v-if="isOnWhatsApp !== null" class="ui message" :class="isOnWhatsApp ? 'positive' : 'negative'">
                <div class="header">
                    <i :class="isOnWhatsApp ? 'check circle icon' : 'times circle icon'"></i>
                    {{ isOnWhatsApp ? 'User is on WhatsApp' : 'User is not on WhatsApp' }}
                </div>
                <p>Phone: {{ phone_id }}</p>
            </div>
        </div>
    </div>
    `
}