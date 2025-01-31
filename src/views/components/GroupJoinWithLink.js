export default {
    name: 'JoinGroupWithLink',
    data() {
        return {
            loading: false,
            link: '',
        }
    },
    methods: {
        openModal() {
            $('#modalGroupJoinWithLink').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (!this.link.trim()) {
                return false;
            }

            // should valid URL
            try {
                new URL(this.link);
            } catch (error) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalGroupJoinWithLink').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let response = await window.http.post(`/group/join-with-link`, {
                    link: this.link,
                })
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
            this.link = '';
        },
    },
    template: `
    <div class="green card" @click="openModal" style="cursor: pointer">
        <div class="content">
            <a class="ui green right ribbon label">Group</a>
            <div class="header">Join Groups</div>
            <div class="description">
                Join group with invitation link
            </div>
        </div>
    </div>
    
    <!--  Modal AccountGroup  -->
    <div class="ui small modal" id="modalGroupJoinWithLink">
        <i class="close icon"></i>
        <div class="header">
            Join Group With Link
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Invitation Link</label>
                    <input v-model="link" type="text"
                           placeholder="Invitation link..."
                           aria-label="Invitation Link">
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                 @click.prevent="handleSubmit" type="button">
                Join
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}