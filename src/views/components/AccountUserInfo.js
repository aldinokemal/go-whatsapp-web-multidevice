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
            resolvedPhone: null,
            resolvedLid: null,
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
                const results = response.data.results;
                const userData = results.data && results.data.length > 0 ? results.data[0] : null;

                if (userData) {
                    this.name = userData.verified_name;
                    this.status = userData.status;
                    this.devices = userData.devices || [];
                }

                this.resolvedPhone = results.resolved_phone || null;
                this.resolvedLid = results.resolved_lid || null;
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
            this.resolvedPhone = null;
            this.resolvedLid = null;
            this.type = window.TYPEUSER;
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
        <a class="ui olive right ribbon label">Account</a>
            <div class="header">User Info</div>
            <div class="description">
                You can search someone user info by phone or LID
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

            <div v-if="devices.length > 0 || resolvedPhone || resolvedLid" class="ui segment" style="margin-top: 1em;">
                <div class="ui list">
                    <div class="item" v-if="resolvedPhone">
                        <i class="phone icon"></i>
                        <div class="content">
                            <div class="header">Resolved Phone</div>
                            <div class="description">{{ resolvedPhone }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="resolvedLid">
                        <i class="linkify icon"></i>
                        <div class="content">
                            <div class="header">Resolved LID</div>
                            <div class="description">{{ resolvedLid }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="name">
                        <i class="user icon"></i>
                        <div class="content">
                            <div class="header">Name</div>
                            <div class="description">{{ name }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="status">
                        <i class="info circle icon"></i>
                        <div class="content">
                            <div class="header">Status</div>
                            <div class="description">{{ status }}</div>
                        </div>
                    </div>
                    <div class="item" v-if="devices.length > 0">
                        <i class="mobile alternate icon"></i>
                        <div class="content">
                            <div class="header">Devices ({{ devices.length }})</div>
                            <div class="ui relaxed list">
                                <div class="item" v-for="d in devices" :key="d.AD">
                                    <i class="tablet icon"></i>
                                    <div class="content">{{ d.Device }} - {{ d.AD }}</div>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
    `
}