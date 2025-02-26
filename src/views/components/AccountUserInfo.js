import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'AccountUserInfo',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            //
            name: null,
            status: null,
            devices: [],
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
            $('#modalUserInfo').modal('show');
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
                await this.submitApi();
                showSuccessInfo("Info fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/user/info?phone=${this.phone_id}`)
                this.name = response.data.results.verified_name;
                this.status = response.data.results.status;
                this.devices = response.data.results.devices;
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
            this.name = null;
            this.status = null;
            this.devices = [];
            this.type = window.TYPEUSER;
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
        <a class="ui olive right ribbon label">Account</a>
            <div class="header">User Info</div>
            <div class="description">
                You can search someone user info by phone
            </div>
        </div>
    </div>
    
    
    <!--  Modal UserInfo  -->
    <div class="ui small modal" id="modalUserInfo">
        <i class="close icon"></i>
        <div class="header">
            Search User Information
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>

                <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !this.isValidForm() || this.loading}"
                        @click.prevent="handleSubmit">
                    Search
                </button>
            </form>

            <div v-if="devices.length > 0" class="center">
                <ol>
                    <li>Name: {{ name }}</li>
                    <li>Status: {{ status }}</li>
                    <li>Device:
                        <ul>
                            <li v-for="d in devices">
                                {{ d.Device }}
                            </li>
                        </ul>
                    </li>
                </ol>
            </div>
        </div>
    </div>
    `
}