export default {
    name: 'AccountUserInfo',
    data() {
        return {
            type: 'user',
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
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        async openModal() {
            this.handleReset();
            $('#modalUserInfo').modal('show');
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
            this.type = 'user';
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer;">
        <div class="content">
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
                <div class="field">
                    <label>Type</label>
                    <select name="type" v-model="type" aria-label="type">
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone</label>
                    <input v-model="phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>

                <button type="button" class="ui primary button" :class="{'loading': loading}"
                        @click="handleSubmit">
                    Search
                </button>
            </form>

            <div v-if="devices.length > 0" class="center">
                <ol>
                    <li>Nama: {{ name }}</li>
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