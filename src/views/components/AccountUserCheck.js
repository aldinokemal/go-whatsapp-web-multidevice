export default {
    name: 'AccountUserCheck',
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            //
            is_in_whatsapp: null,
            jid: null,
            verified_name: null,
            query: null,
            code: null,
            //
            loading: false,
        }
    },

    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        openModal() {
            $('#modalUserCheck').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (!this.phone.trim()) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/user/check?phone=${this.phone_id}`)
                this.is_in_whatsapp = response.data.results.IsInWhatsapp;
                this.jid = response.data.results.jid;
                this.verified_name = response.data.results.verified_name;
                this.query = response.data.results.query;
                this.code = response.code;
            } catch (error) {
                if (error.response) {
                    this.verified_name = null;
                    this.jid = null;
                    throw new Error(error.response.data.message);
                }
                this.verified_name = null;
                this.jid = null;
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.is_in_whatsapp = null;
            this.jid = null;
            this.verified_name = null;
            this.query = null;
            this.code = null;
            this.type = window.TYPEUSER;
        }
    },
    template: `
    <div class="green card" @click="openModal()" style="cursor: pointer;">
        <div class="content">
            <a class="ui olive right ribbon label">Account</a>
            <div class="header">User Check</div>
            <div class="description">
                You can check if the user exists on whatapp
            </div>
        </div>
    </div>
    
    
    <!--  Modal UserCheck  -->
    <div class="ui small modal" id="modalUserCheck">
        <i class="close icon"></i>
        <div class="header">
            Search User Information
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Phone</label>
                    <input v-model="phone" type="text"
                           placeholder="Type your phone number"
                           aria-label="Phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>

            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Search
                <i class="search icon"></i>
            </button>
        </div>

        <div v-if="is_in_whatsapp != null" class="center">
            <ol>
                <li>Name: {{ verified_name }}</li>
                <li>JID: {{ jid }}</li>
            </ol>
        </div>
        <div v-else class="center">
            <div v-if="code == 'INVALID_JID'" class="center">
                <ol>
                    <li>Name: {{ verified_name }}</li>
                    <li>JID: {{ jid }}</li>
                </ol>
            </div>
        </div>
    </div>
    `
}