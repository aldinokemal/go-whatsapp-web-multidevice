import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'AccountAvatar',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            image: null,
            loading: false,
            is_preview: false,
            is_community: false,
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
            $('#modalUserAvatar').modal('show');
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
                showSuccessInfo("Avatar fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.get(`/user/avatar?phone=${this.phone_id}&is_preview=${this.is_preview}&is_community=${this.is_community}`)
                this.image = response.data.results.url;
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
            this.image = null;
            this.type = window.TYPEUSER;
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
        <a class="ui olive right ribbon label">Account</a>
            <div class="header">Avatar</div>
            <div class="description">
                You can search someone avatar by phone
            </div>
        </div>
    </div>

    <!--  Modal UserAvatar  -->
    <div class="ui small modal" id="modalUserAvatar">
        <i class="close icon"></i>
        <div class="header">
            Search User Avatar
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>

                <div class="field">
                    <label>Preview</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="compress" v-model="is_preview">
                        <label>Check for small size image</label>
                    </div>
                </div>

                <div class="field">
                    <label>Community</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="compress" v-model="is_community">
                        <label>Check is it's community image</label>
                    </div>
                </div>

                <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !this.isValidForm() || this.loading}"
                        @click.prevent="handleSubmit">
                    Search
                </button>
            </form>

            <div v-if="image != null" class="center">
                <img :src="image" alt="profile picture" style="padding-top: 10px; max-height: 200px">
            </div>
        </div>
    </div>
    `
}