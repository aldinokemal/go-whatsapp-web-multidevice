export default {
    name: 'GroupInfo',
    components: {},
    data() {
        return {
            group_id: '',
            groupInfo: null,
            loading: false,
        }
    },
    computed: {
        fullGroupID() {
            if (!this.group_id) return '';
            // Ensure suffix
            if (this.group_id.endsWith(window.TYPEGROUP)) {
                return this.group_id;
            }
            return this.group_id + window.TYPEGROUP;
        }
    },
    methods: {
        openModal() {
            this.reset();
            $('#modalGroupInfo').modal('show');
        },
        isValidForm() {
            return this.group_id.trim() !== '';
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) return;
            try {
                await this.fetchInfo();
                showSuccessInfo('Group info fetched');
            } catch (err) {
                showErrorInfo(err.message || err);
            }
        },
        async fetchInfo() {
            this.loading = true;
            try {
                const response = await window.http.get(`/user/info-group?group_id=${this.fullGroupID}`);
                this.groupInfo = response.data.results;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        reset() {
            this.group_id = '';
            this.groupInfo = null;
            this.loading = false;
        }
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer;">
        <div class="content">
            <a class="ui olive right ribbon label">Group</a>
            <div class="header">Group Info</div>
            <div class="description">
                Search information about a group by ID
            </div>
        </div>
    </div>

    <!-- Modal -->
    <div class="ui small modal" id="modalGroupInfo">
        <i class="close icon"></i>
        <div class="header">Search Group Information</div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Group ID</label>
                    <input v-model="group_id" placeholder="e.g. 1203630...">
                    <input :value="fullGroupID" disabled>
                </div>
                <button type="button" class="ui primary button" :class="{'loading': loading, 'disabled': !isValidForm() || loading}" @click.prevent="handleSubmit">Search</button>
            </form>

            <div v-if="groupInfo" style="margin-top: 1rem;">
                <pre style="white-space: pre-wrap;">{{ JSON.stringify(groupInfo, null, 2) }}</pre>
            </div>
        </div>
    </div>
    `
}