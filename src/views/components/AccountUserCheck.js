import FormCheckUserRecipient from "./generic/FormCheckUserRecipient.js";

export default {
    name: 'AccountUserCheck',
    components: {
        FormCheckUserRecipient
    },
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
        async openModal() {
            this.handleReset();
            $('#modalUserCheck').modal('show');
        },
        async handleSubmit() {
            try {
                await this.submitApi();
                showSuccessInfo("Info fetched")
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
    <div class="green card" @click="openModal" style="cursor: pointer;">
        <div class="content">
            <div class="header">User Check</div>
            <div class="description">
                You can check if the user exists on whatapp
            </div>
        </div>
    </div>
    
    
    <!--  Modal UserInfo  -->
    <div class="ui small modal" id="modalUserCheck">
        <i class="close icon"></i>
        <div class="header">
            Search User Information
        </div>
        <div class="content">
            <form class="ui form">
                <FormCheckUserRecipient v-model:type="type" v-model:phone="phone"/>

                <button type="button" class="ui primary button" :class="{'loading': loading}"
                        @click="handleSubmit">
                    Search
                </button>
            </form>

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
    </div>
    `
}