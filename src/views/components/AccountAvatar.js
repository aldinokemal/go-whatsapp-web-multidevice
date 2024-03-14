export default {
    name: 'AccountAvatar',
    data() {
        return {
            type: 'user',
            phone: '',
            image: null,
            loading: false,
            is_preview: false,
            is_community: false,
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
            $('#modalUserAvatar').modal('show');
        },
        async handleSubmit() {
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
            this.type = 'user';
        }
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer;">
        <div class="content">
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
                <div class="field">
                    <label>Type</label>
                    <select name="type" v-model="type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone</label>
                    <input v-model="phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>

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

                <button type="button" class="ui primary button" :class="{'loading': loading}"
                        @click="handleSubmit">
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