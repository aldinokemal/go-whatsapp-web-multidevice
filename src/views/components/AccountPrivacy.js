export default {
    name: 'AccountPrivacy',
    data() {
        return {
            data_privacy: null
        }
    },
    methods: {
        async openModal() {
            try {
                await this.submitApi();
                $('#modalUserPrivacy').modal('show');
                showSuccessInfo("Privacy fetched")
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            try {
                let response = await window.http.get(`/user/my/privacy`)
                this.data_privacy = response.data.results;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            }
        },
    },
    template: `
    <div class="olive card" @click="openModal" style="cursor: pointer">
        <div class="content">
        <a class="ui olive right ribbon label">Account</a>
            <div class="header">My Privacy Setting</div>
            <div class="description">
                Get your privacy settings
            </div>
        </div>
    </div>
    
    <!--  Modal UserPrivacy  -->
    <div class="ui small modal" id="modalUserPrivacy">
        <i class="close icon"></i>
        <div class="header">
            My Privacy
        </div>
        <div class="content">
            <ol v-if="data_privacy != null">
                <li>Who can add Group : <b>{{ data_privacy.group_add }}</b></li>
                <li>Who can see my Last Seen : <b>{{ data_privacy.last_seen }}</b></li>
                <li>Who can see my Status : <b>{{ data_privacy.status }}</b></li>
                <li>Who can see my Profile : <b>{{ data_privacy.profile }}</b></li>
                <li>Read Receipts : <b>{{ data_privacy.read_receipts }}</b></li>
            </ol>
        </div>
    </div>
    `
}